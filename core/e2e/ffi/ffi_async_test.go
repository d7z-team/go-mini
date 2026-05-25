package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type asyncBridge struct{}

func (asyncBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	switch req.MethodID {
	case 1:
		return ffigo.AsyncValue[int64](
			ffigo.AsyncFunc[int64](func(_ context.Context, done ffigo.Completion[int64]) (ffigo.WaitHandle, error) {
				timer := time.AfterFunc(time.Millisecond, func() {
					done.Complete(42, nil)
				})
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "async.Value", func() { timer.Stop() }), nil
			}),
			func(buf *ffigo.Buffer, value int64) error {
				buf.WriteVarint(value)
				return nil
			},
		), nil
	case 2:
		args := append([]byte(nil), req.Args...)
		return ffigo.AsyncValue[ffigo.Void](
			ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
				timer := time.AfterFunc(time.Millisecond, func() {
					done.Complete(ffigo.Void{}, nil)
				})
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "async.Mutate", func() { timer.Stop() }), nil
			}),
			func(buf *ffigo.Buffer, _ ffigo.Void) error {
				reader := ffigo.NewReader(args)
				mutated := append([]byte(strings.ToUpper(string(reader.ReadBytes()))), '!')
				buf.WriteUvarint(1)
				buf.WriteBytes(mutated)
				return nil
			},
		), nil
	case 3:
		return ffigo.AsyncValue[int64](
			ffigo.AsyncFunc[int64](func(_ context.Context, done ffigo.Completion[int64]) (ffigo.WaitHandle, error) {
				timer := time.AfterFunc(time.Millisecond, func() {
					done.Complete(0, errors.New("async failure"))
				})
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "async.Fail", func() { timer.Stop() }), nil
			}),
			func(buf *ffigo.Buffer, value int64) error {
				buf.WriteVarint(value)
				return nil
			},
		), nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (asyncBridge) Invoke(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, fmt.Errorf("unexpected invoke %s", req.Method)
}

func (asyncBridge) DestroyHandle(uint32) error {
	return nil
}

func TestAsyncFFIResumesWithReturnAndCopyBack(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := asyncBridge{}
	executor.RegisterFFISchema("async.Value", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Int64"), "")
	executor.RegisterFFISchema(
		"async.Mutate",
		bridge,
		2,
		runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", runtime.FFIParamInOutBytes),
		"",
	)

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "async"

func main() {
	v := async.Value()
	if v != 42 {
		panic("unexpected async value")
	}
	buf := []byte("go")
	async.Mutate(buf)
	if String(buf) != "GO!" {
		panic("async copy-back failed")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncFFICompletionErrorPropagates(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := asyncBridge{}
	executor.RegisterFFISchema("async.Fail", bridge, 3, runtime.MustParseRuntimeFuncSig("function() Int64"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "async"

func main() {
	_ = async.Fail()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "async failure") {
		t.Fatalf("expected async completion error, got %T %v", err, err)
	}
}

type burstAsyncBridge struct {
	target      int64
	release     chan struct{}
	releaseOnce sync.Once
	started     atomic.Int64
	attempted   atomic.Int64
	cancelled   atomic.Int64
}

func newBurstAsyncBridge(target int64) *burstAsyncBridge {
	return &burstAsyncBridge{
		target:  target,
		release: make(chan struct{}),
	}
}

func (b *burstAsyncBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	switch req.MethodID {
	case 1:
		return ffigo.AsyncValue[ffigo.Void](
			ffigo.AsyncFunc[ffigo.Void](func(ctx context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
				cancelled := make(chan struct{})
				var cancelOnce sync.Once
				b.started.Add(1)
				go func() {
					select {
					case <-b.release:
						done.Complete(ffigo.Void{}, nil)
						b.attempted.Add(1)
					case <-ctx.Done():
						done.Complete(ffigo.Void{}, ctx.Err())
						b.attempted.Add(1)
					case <-cancelled:
					}
				}()
				cancel := func() {
					cancelOnce.Do(func() {
						b.cancelled.Add(1)
						close(cancelled)
					})
				}
				return ffigo.NewWaitHandle(ffigo.WaitDependsOnVM, "gate.Wait", cancel), nil
			}),
			func(*ffigo.Buffer, ffigo.Void) error { return nil },
		), nil
	case 2:
		return ffigo.AsyncValue[int64](
			ffigo.AsyncFunc[int64](func(_ context.Context, done ffigo.Completion[int64]) (ffigo.WaitHandle, error) {
				go done.Complete(b.started.Load(), nil)
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "gate.Started", nil), nil
			}),
			func(buf *ffigo.Buffer, v int64) error {
				buf.WriteVarint(v)
				return nil
			},
		), nil
	case 3:
		return ffigo.AsyncValue[ffigo.Void](
			ffigo.AsyncFunc[ffigo.Void](func(ctx context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
				b.releaseOnce.Do(func() { close(b.release) })
				cancelled := make(chan struct{})
				var cancelOnce sync.Once
				go func() {
					deadline := time.After(2 * time.Second)
					for b.attempted.Load() < b.target {
						select {
						case <-deadline:
							done.Complete(ffigo.Void{}, fmt.Errorf("timed out waiting for async completion attempts: got %d want %d", b.attempted.Load(), b.target))
							return
						case <-ctx.Done():
							done.Complete(ffigo.Void{}, ctx.Err())
							return
						case <-cancelled:
							return
						default:
							time.Sleep(time.Millisecond)
						}
					}
					done.Complete(ffigo.Void{}, nil)
				}()
				cancel := func() {
					cancelOnce.Do(func() { close(cancelled) })
				}
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "gate.Release", cancel), nil
			}),
			func(*ffigo.Buffer, ffigo.Void) error { return nil },
		), nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (b *burstAsyncBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b *burstAsyncBridge) DestroyHandle(uint32) error {
	return nil
}

func TestAsyncFFICompletionBurstDoesNotDropTokens(t *testing.T) {
	const workers = 1200

	executor := engine.NewMiniExecutor()
	bridge := newBurstAsyncBridge(workers)
	executor.RegisterFFISchema("gate.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")
	executor.RegisterFFISchema("gate.Started", bridge, 2, runtime.MustParseRuntimeFuncSig("function() Int64"), "")
	executor.RegisterFFISchema("gate.Release", bridge, 3, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "gate"

var done = 0

func worker() {
	gate.Wait()
	done = done + 1
}

func main() {
	for i := 0; i < 1200; i++ {
		go worker()
	}
	for gate.Started() < 1200 {
	}
	gate.Release()
	if done != 1200 {
		panic("lost async completion")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := bridge.cancelled.Load(); got != 0 {
		t.Fatalf("burst completions should not be cancelled, got %d cancellations", got)
	}
}

type blockingAsyncBridge struct {
	cancelled atomic.Int64
	kind      ffigo.WaitKind
	reason    string
}

func (b *blockingAsyncBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.MethodID != 1 {
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
	return ffigo.AsyncValue[ffigo.Void](
		ffigo.AsyncFunc[ffigo.Void](func(context.Context, ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
			return ffigo.NewWaitHandle(b.kind, b.reason, func() { b.cancelled.Add(1) }), nil
		}),
		func(*ffigo.Buffer, ffigo.Void) error { return nil },
	), nil
}

func (b *blockingAsyncBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b *blockingAsyncBridge) DestroyHandle(uint32) error {
	return nil
}

func TestAsyncFFICancelledOnContextDeadline(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &blockingAsyncBridge{kind: ffigo.WaitExternal, reason: "external block"}
	executor.RegisterFFISchema("block.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = prog.Execute(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %T %v", err, err)
	}
	if got := bridge.cancelled.Load(); got != 1 {
		t.Fatalf("expected pending async FFI cancel once, got %d", got)
	}
}

func TestAsyncFFICancelledOnVMPanic(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &blockingAsyncBridge{kind: ffigo.WaitExternal, reason: "external block"}
	executor.RegisterFFISchema("block.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "block"

func waiter(ready chan Int64) {
	ready <- 1
	block.Wait()
}

func main() {
	ready := make(chan Int64)
	go waiter(ready)
	<-ready
	panic("fatal after async wait started")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fatal after async wait started") {
		t.Fatalf("expected VM panic, got %T %v", err, err)
	}
	if got := bridge.cancelled.Load(); got != 1 {
		t.Fatalf("expected pending async FFI cancel once, got %d", got)
	}
}

func TestAsyncFFIAllBlockedReportsWaits(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &blockingAsyncBridge{kind: ffigo.WaitDependsOnVM, reason: "test blocked on VM"}
	executor.RegisterFFISchema("block.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

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

	err = prog.Execute(context.Background())
	var blocked *runtime.VMAllBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected VM all-blocked error, got %T %v", err, err)
	}
	if len(blocked.Waits) != 1 {
		t.Fatalf("expected one blocked wait, got %#v", blocked.Waits)
	}
	wait := blocked.Waits[0]
	if wait.RouteName != "block.Wait" || wait.MethodID != 1 || wait.Reason != "test blocked on VM" {
		t.Fatalf("unexpected blocked wait: %#v", wait)
	}
	if got := bridge.cancelled.Load(); got != 1 {
		t.Fatalf("expected pending async FFI cancel once, got %d", got)
	}
}
