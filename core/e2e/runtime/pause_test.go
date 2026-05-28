package tests

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestRunHandlePauseAndResume(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

var Counter = 0

func main() {
	for true {
		Counter = Counter + 1
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	run, err := prog.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForGlobalAtLeast(t, prog, "Counter", 1, time.Second)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	waitForRunPhase(t, run, runtime.RunPhasePaused, time.Second)
	counterBefore := loadInt64Global(t, prog, "Counter")
	time.Sleep(50 * time.Millisecond)
	counterAfter := loadInt64Global(t, prog, "Counter")
	if counterAfter != counterBefore {
		t.Fatalf("expected paused VM counter to stay fixed, got %d then %d", counterBefore, counterAfter)
	}
	if err := run.Resume(); err != nil {
		t.Fatal(err)
	}
	waitForGlobalAtLeast(t, prog, "Counter", counterAfter+1, time.Second)
	cancel()
	if err := run.Wait(); err == nil || (!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)) {
		t.Fatalf("expected canceled run after resume, got %v", err)
	}
	if final := loadInt64Global(t, prog, "Counter"); final <= counterAfter {
		t.Fatalf("expected resumed VM to continue, got final=%d paused=%d", final, counterAfter)
	}
}

func TestPauseWaitsForSyncFFISafePoint(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	testsurface.UseRoute(t, executor, "host.Block", syncBlockBridge{delay: 80 * time.Millisecond}, 1, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "host"

var Phase = 0

func main() {
	Phase = 1
	host.Block(80000000)
	Phase = 2
	for true {
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	run, err := prog.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForGlobalAtLeast(t, prog, "Phase", 1, time.Second)
	time.Sleep(10 * time.Millisecond)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	waitForRunPhase(t, run, runtime.RunPhasePausing, time.Second)
	time.Sleep(20 * time.Millisecond)
	if phase := run.Phase(); phase != runtime.RunPhasePausing {
		t.Fatalf("expected pause request to wait for sync FFI safe point, got phase %d", phase)
	}
	if phase := loadInt64Global(t, prog, "Phase"); phase != 1 {
		t.Fatalf("expected VM to remain before post-FFI work while blocking, got phase %d", phase)
	}
	waitForRunPhase(t, run, runtime.RunPhasePaused, time.Second)
	if phase := loadInt64Global(t, prog, "Phase"); phase != 1 {
		t.Fatalf("expected safe-point pause before next instruction, got phase %d", phase)
	}
	if err := run.Resume(); err != nil {
		t.Fatal(err)
	}
	waitForGlobalAtLeast(t, prog, "Phase", 2, time.Second)
	cancel()
	if err := run.Wait(); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled run after sync FFI pause test, got %v", err)
	}
}

func TestPausePreemptsAllBlockedUntilResume(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	bridge := &gatedBlockedBridge{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	testsurface.UseRoute(t, executor, "block.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "block"

func main() {
	block.Wait()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	run, err := prog.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-bridge.started:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for async wait setup")
	}
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	close(bridge.release)
	waitForRunPhase(t, run, runtime.RunPhasePaused, time.Second)
	if err := run.Resume(); err != nil {
		t.Fatal(err)
	}
	err = run.Wait()
	var blocked *runtime.VMAllBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected VMAllBlockedError after resume, got %v", err)
	}
	if got := bridge.cancelled.Load(); got != 1 {
		t.Fatalf("expected pending blocked wait cancel once, got %d", got)
	}
}

func waitForGlobalAtLeast(t *testing.T, prog *engine.ExecutableProgram, name string, minValue int64, timeout time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if value, ok := prog.SharedState().LoadGlobal(name); ok && value != nil && value.I64 >= minValue {
			return value.I64
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for global %s >= %d", name, minValue)
	return 0
}

func waitForRunPhase(t *testing.T, run *runtime.RunHandle, want runtime.RunPhase, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if run.Phase() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for run phase %d, got %d", want, run.Phase())
}

type syncBlockBridge struct {
	delay time.Duration
}

type gatedBlockedBridge struct {
	started   chan struct{}
	release   chan struct{}
	cancelled atomic.Int64
}

func (b *gatedBlockedBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.MethodID != 1 {
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
	return ffigo.AsyncValue[ffigo.Void](
		ffigo.AsyncFunc[ffigo.Void](func(context.Context, ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
			close(b.started)
			<-b.release
			return ffigo.NewWaitHandle(ffigo.WaitDependsOnVM, "gate blocked on VM", func() { b.cancelled.Add(1) }), nil
		}),
		func(*ffigo.Buffer, ffigo.Void) error { return nil },
	), nil
}

func (b *gatedBlockedBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b *gatedBlockedBridge) DestroyHandle(uint32) error {
	return nil
}

func (b syncBlockBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.MethodID != 1 {
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
	time.Sleep(b.delay)
	return nil, nil
}

func (b syncBlockBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b syncBlockBridge) DestroyHandle(uint32) error {
	return nil
}
