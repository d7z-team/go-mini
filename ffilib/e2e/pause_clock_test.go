package e2e_test

import (
	"context"
	"testing"
	"time"

	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestPauseFreezesTimeSleep(t *testing.T) {
	executor := newStdExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "time"

var Phase = 0

func main() {
	Phase = 1
	time.Sleep(120 * time.Millisecond)
	Phase = 2
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
	waitForPhase(t, prog, 1, time.Second)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if phase := readGlobalInt(t, prog, "Phase"); phase != 1 {
		t.Fatalf("expected paused sleep to stay in phase 1, got %d", phase)
	}
	resumeAt := time.Now()
	if err := run.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(resumeAt); elapsed < 40*time.Millisecond {
		t.Fatalf("expected resumed sleep to preserve remaining VM wait, took %s", elapsed)
	}
	if phase := readGlobalInt(t, prog, "Phase"); phase != 2 {
		t.Fatalf("expected resumed sleep to finish in phase 2, got %d", phase)
	}
}

func TestPauseDoesNotFreezeContextTimeout(t *testing.T) {
	executor := newStdExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "context"
import "time"

var Phase = 0
var Expired = false

func main() {
	Phase = 1
	ctx, _ := context.WithTimeout(context.Background(), 20 * time.Millisecond)
	Phase = 2
	<-ctx.Done()
	Expired = ctx.Err() == context.DeadlineExceeded
	Phase = 3
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
	waitForPhase(t, prog, 2, time.Second)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if phase := readGlobalInt(t, prog, "Phase"); phase != 2 {
		t.Fatalf("expected paused timeout to stay in phase 2, got %d", phase)
	}
	if expired := readGlobalBool(t, prog, "Expired"); expired {
		t.Fatal("expected paused VM state to remain unchanged before resume")
	}
	resumeAt := time.Now()
	if err := run.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(resumeAt); elapsed > 15*time.Millisecond {
		t.Fatalf("expected real-time context timeout to finish immediately after resume, took %s", elapsed)
	}
	if phase := readGlobalInt(t, prog, "Phase"); phase != 3 {
		t.Fatalf("expected resumed timeout to finish in phase 3, got %d", phase)
	}
	if expired := readGlobalBool(t, prog, "Expired"); !expired {
		t.Fatal("expected resumed timeout to expire")
	}
}

func TestPauseDoesNotFreezeTimeNow(t *testing.T) {
	executor := newStdExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "time"

var Phase = 0
var Before = Int64(0)
var After = Int64(0)

func main() {
	Before = time.Now().UnixNano()
	Phase = 1
	time.Sleep(20 * time.Millisecond)
	After = time.Now().UnixNano()
	Phase = 2
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
	waitForPhase(t, prog, 1, time.Second)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := run.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(); err != nil {
		t.Fatal(err)
	}
	before := readGlobalInt(t, prog, "Before")
	after := readGlobalInt(t, prog, "After")
	if after <= before {
		t.Fatalf("expected real wall clock to move forward, before=%d after=%d", before, after)
	}
	if delta := after - before; delta < int64(40*time.Millisecond) {
		t.Fatalf("expected pause duration to be reflected in time.Now, delta=%d", delta)
	}
}

func waitForPhase(t *testing.T, prog interface {
	SharedState() *runtime.SharedStateSnapshot
}, phase int64, timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got, ok := prog.SharedState().LoadGlobal("Phase"); ok && got != nil && got.I64 >= phase {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for phase %d", phase)
}

func readGlobalInt(t *testing.T, prog interface {
	SharedState() *runtime.SharedStateSnapshot
}, name string,
) int64 {
	t.Helper()
	value, ok := prog.SharedState().LoadGlobal(name)
	if !ok || value == nil {
		t.Fatalf("missing global %s", name)
	}
	return value.I64
}

func readGlobalBool(t *testing.T, prog interface {
	SharedState() *runtime.SharedStateSnapshot
}, name string,
) bool {
	t.Helper()
	value, ok := prog.SharedState().LoadGlobal(name)
	if !ok || value == nil {
		t.Fatalf("missing global %s", name)
	}
	return value.Bool
}
