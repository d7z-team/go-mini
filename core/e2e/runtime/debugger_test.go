package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/debugger"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func loadInt64Global(t *testing.T, prog *engine.ExecutableProgram, name string) int64 {
	t.Helper()
	value, ok := prog.SharedState().LoadGlobal(name)
	if !ok || value == nil {
		t.Fatalf("missing global %s", name)
	}
	return value.I64
}

func startDebugRun(ctx context.Context, t *testing.T, prog *engine.ExecutableProgram) (*runtime.RunHandle, <-chan error) {
	t.Helper()
	run, err := prog.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- run.Wait()
	}()
	return run, done
}

type debugEventResult struct {
	event *debugger.Event
	err   error
}

func nextDebugEvent(ctx context.Context, t *testing.T, dbg *debugger.Session) *debugger.Event {
	t.Helper()
	event, err := dbg.NextEvent(ctx)
	if err != nil {
		t.Fatalf("next debugger event failed: %v", err)
	}
	return event
}

func nextDebugEventAsync(ctx context.Context, dbg *debugger.Session) <-chan debugEventResult {
	ch := make(chan debugEventResult, 1)
	go func() {
		event, err := dbg.NextEvent(ctx)
		ch <- debugEventResult{event: event, err: err}
	}()
	return ch
}

func TestDebuggerBasicBreakAndStep(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
	package main
	func main() {
		a := 10       // Line 4
		b := 20       // Line 5
		c := a + b    // Line 6
	}
	`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)

	event := nextDebugEvent(ctx, t, dbg)
	if phase := run.Phase(); phase != runtime.RunPhasePaused {
		t.Fatalf("expected run to be paused before debugger event delivery, got phase %d", phase)
	}
	if event.RunID != run.ID() {
		t.Fatalf("expected run id %d, got %d", run.ID(), event.RunID)
	}
	if event.ExecutionContextID != 1 {
		t.Fatalf("expected root execution context id 1, got %d", event.ExecutionContextID)
	}
	if event.Loc.L != 5 {
		t.Fatalf("expected break at line 5, got %d", event.Loc.L)
	}
	if event.Variables["a"] != "10" {
		t.Fatalf("expected a=10, got %v", event.Variables["a"])
	}
	if _, exists := event.Variables["b"]; exists {
		t.Fatalf("b should not be initialized yet")
	}
	if err := run.StepInto(); err != nil {
		t.Fatal(err)
	}

	for {
		event := nextDebugEvent(ctx, t, dbg)
		if event.Loc.L == 6 {
			if event.Variables["b"] != "20" {
				t.Fatalf("expected b=20, got %v", event.Variables["b"])
			}
			if err := run.Continue(); err != nil {
				t.Fatal(err)
			}
			goto WAIT_DONE
		}
		if event.Loc.L == 5 {
			if err := run.StepInto(); err != nil {
				t.Fatal(err)
			}
			continue
		}
		t.Fatalf("unexpected step to line %d", event.Loc.L)
	}

WAIT_DONE:
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for program completion")
	}
}

func TestDebuggerSnippetMode(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()

	sourceSnippet := `
		x := 100 // Line 2
		y := 200 // Line 3
		z := x * y
	`

	dbg := debugger.NewSession()
	for line := 1; line <= 8; line++ {
		dbg.AddBreakpoint(line)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, err := testExecutor.StartExecute(ctx, sourceSnippet, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- run.Wait()
	}()

	linesSeen := []int{}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	eventCh := nextDebugEventAsync(ctx, dbg)
	for {
		select {
		case result := <-eventCh:
			if result.err != nil {
				t.Fatalf("next debugger event failed: %v", result.err)
			}
			linesSeen = append(linesSeen, result.event.Loc.L)
			if err := run.StepInto(); err != nil {
				t.Fatal(err)
			}
			eventCh = nextDebugEventAsync(ctx, dbg)
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			goto DONE
		case <-ctx.Done():
			t.Fatal("timeout in snippet debugger test")
		case <-ticker.C:
		}
	}

DONE:
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}

	if len(linesSeen) == 0 {
		t.Fatalf("no statements were intercepted in snippet mode")
	}
}

func TestDebuggerStepStateIsClearedWhenRunEnds(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	firstProgram, err := testExecutor.NewRuntimeByGoCode(`
package main
func main() {
	x := 1
	_ = x // Line 5
}
`)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, firstProgram)
	event := nextDebugEvent(ctx, t, dbg)
	if event.Loc.L != 5 {
		t.Fatalf("expected breakpoint at line 5, got %d", event.Loc.L)
	}
	if err := run.StepInto(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if dbg.HasStep(run.ID()) {
		t.Fatal("expected debugger step state to be cleared when run ends")
	}

	dbg.RemoveBreakpoint(5)
	secondProgram, err := testExecutor.NewRuntimeByGoCode(`
package main
func main() {
	x := 10
	y := x + 1
	_ = y
}
`)
	if err != nil {
		t.Fatal(err)
	}

	secondRun, secondDone := startDebugRun(ctx, t, secondProgram)
	eventCh := nextDebugEventAsync(ctx, dbg)
	select {
	case result := <-eventCh:
		if result.err != nil {
			t.Fatalf("next debugger event failed: %v", result.err)
		}
		t.Fatalf("unexpected debugger event from cleared step state: run=%d line=%d", result.event.RunID, result.event.Loc.L)
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for second run completion")
	}
	if dbg.HasStep(secondRun.ID()) {
		t.Fatal("second run should not leave an active step state")
	}
}

func TestDebuggerLoopExecution(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
	package main
	func main() {
		sum := 0       // Line 4
		for i := 1; i <= 3; i++ { // Line 5
			sum = sum + i // Line 6
		}
		return         // Line 8
	}
	`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(6)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)

	expectedI := []string{"1", "2", "3"}
	expectedSum := []string{"0", "1", "3"}
	for loopCount := 0; loopCount < 3; loopCount++ {
		event := nextDebugEvent(ctx, t, dbg)
		if event.Loc.L != 6 {
			t.Fatalf("expected break at line 6, got %d", event.Loc.L)
		}
		if actual := event.Variables["i"]; actual != expectedI[loopCount] {
			t.Errorf("loop %d: expected i=%s, got %s", loopCount, expectedI[loopCount], actual)
		}
		if actual := event.Variables["sum"]; actual != expectedSum[loopCount] {
			t.Errorf("loop %d: expected sum=%s, got %s", loopCount, expectedSum[loopCount], actual)
		}
		if err := run.Continue(); err != nil {
			t.Fatal(err)
		}
	}

	noEventCtx, noEventCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer noEventCancel()
	if event, err := dbg.NextEvent(noEventCtx); err == nil {
		t.Fatalf("breakpoint hit more times than expected: %#v", event)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error while checking for extra breakpoint: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerAnytimePause(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
	package main
	func main() {
		sum := 0
		for i := 0; i < 200000; i++ {
			sum = sum + 1
		}
	}
	`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)
	time.Sleep(10 * time.Millisecond)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	waitForRunPhase(t, run, runtime.RunPhasePaused, time.Second)
	if err := run.Continue(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := run.Pause(runtime.PauseReason{Kind: "test"}); err != nil {
		t.Fatal(err)
	}
	waitForRunPhase(t, run, runtime.RunPhasePaused, time.Second)
	if err := run.Continue(); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerContextBreakpointHitsChildContext(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
package main

func worker() Int64 {
	x := 1 // Line 5
	return x
}

func main() {
	go worker()
	for i := 0; i < 1000; i++ {
		anchor := i
		_ = anchor
	}
}
`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)

	event := nextDebugEvent(ctx, t, dbg)
	if event.Loc.L != 5 {
		t.Fatalf("expected child execution context breakpoint at line 5, got %d", event.Loc.L)
	}
	if event.ExecutionContextID <= 1 {
		t.Fatalf("expected child execution context id greater than root, got %d", event.ExecutionContextID)
	}
	if err := run.Continue(); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerContextBreakpointHitsMultipleContexts(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
package main

func worker() Int64 {
	x := 1 // Line 5
	return x
}

func main() {
	go worker()
	go worker()
	for i := 0; i < 1000; i++ {
		anchor := i
		_ = anchor
	}
}
`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)

	hits := 0
	contextHits := make(map[uint32]bool)
	eventCh := nextDebugEventAsync(ctx, dbg)
	for {
		select {
		case result := <-eventCh:
			if result.err != nil {
				t.Fatalf("next debugger event failed: %v", result.err)
			}
			event := result.event
			if event.Loc.L != 5 {
				t.Fatalf("expected child execution context breakpoint at line 5, got %d", event.Loc.L)
			}
			if event.ExecutionContextID <= 1 {
				t.Fatalf("expected child execution context id greater than root, got %d", event.ExecutionContextID)
			}
			hits++
			contextHits[event.ExecutionContextID] = true
			if err := run.Continue(); err != nil {
				t.Fatal(err)
			}
			eventCh = nextDebugEventAsync(ctx, dbg)
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if hits < 2 {
				t.Fatalf("expected at least 2 child execution context breakpoint hits, got %d", hits)
			}
			if len(contextHits) < 2 {
				t.Fatalf("expected breakpoints from at least 2 child execution contexts, got ids %v", contextHits)
			}
			return
		case <-ctx.Done():
			t.Fatal("timeout waiting for multi-context debugger completion")
		}
	}
}

func TestDebuggerContextBreakpointUsesAllStopPause(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
package main

var ticks = 0

func breaker() {
	for i := 0; i < 1000; i++ {
		marker := i
		_ = marker
	}
	paused := 1 // Line 11
	_ = paused
}

func runner() {
	for i := 0; i < 50000; i++ {
		ticks = ticks + 1
	}
}

func main() {
	go runner()
	go breaker()
	for i := 0; i < 50000; i++ {
		anchor := i
		_ = anchor
	}
}
`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(11)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)

	event := nextDebugEvent(ctx, t, dbg)
	if event.Loc.L != 11 {
		t.Fatalf("expected child execution context breakpoint at line 11, got %d", event.Loc.L)
	}
	if event.ExecutionContextID <= 1 {
		t.Fatalf("expected child execution context id greater than root, got %d", event.ExecutionContextID)
	}
	ticksBefore := loadInt64Global(t, testProgram, "ticks")
	time.Sleep(50 * time.Millisecond)
	ticksAfter := loadInt64Global(t, testProgram, "ticks")
	if ticksAfter != ticksBefore {
		t.Fatalf("expected all-stop pause to freeze other execution contexts, ticks changed from %d to %d", ticksBefore, ticksAfter)
	}
	if err := run.Continue(); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerRemoveBreakpointAfterHitPreventsLaterHits(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
package main
func main() {
	total := 0
	for i := 0; i < 3; i++ {
		total = total + 1 // Line 6
	}
}
`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(6)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, done := startDebugRun(ctx, t, testProgram)

	event := nextDebugEvent(ctx, t, dbg)
	if event.Loc.L != 6 {
		t.Fatalf("expected first breakpoint at line 6, got %d", event.Loc.L)
	}
	dbg.RemoveBreakpoint(6)
	if err := run.Continue(); err != nil {
		t.Fatal(err)
	}

	eventCh := nextDebugEventAsync(ctx, dbg)
	select {
	case result := <-eventCh:
		if result.err != nil {
			t.Fatalf("next debugger event failed: %v", result.err)
		}
		t.Fatalf("removed breakpoint should not pause again, got line %d", result.event.Loc.L)
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for completion after breakpoint removal")
	}
}

func TestDebuggerConcurrentBreakpointMutation(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
package main
func main() {
	total := 0
	for i := 0; i < 20000; i++ {
		total = total + 1 // Line 6
	}
}
`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	stopMutating := make(chan struct{})
	mutatorDone := make(chan struct{})
	go func() {
		defer close(mutatorDone)
		for {
			select {
			case <-stopMutating:
				return
			case <-ctx.Done():
				return
			default:
			}
			dbg.AddBreakpoint(6)
			_ = dbg.HasBreakpoint(6)
			dbg.RemoveBreakpoint(6)
		}
	}()

	done := make(chan error, 1)
	run, err := testProgram.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		err := run.Wait()
		close(stopMutating)
		done <- err
	}()

	eventCh := nextDebugEventAsync(ctx, dbg)
	for {
		select {
		case result := <-eventCh:
			if result.err != nil {
				t.Fatalf("next debugger event failed: %v", result.err)
			}
			if err := run.Continue(); err != nil {
				t.Fatal(err)
			}
			eventCh = nextDebugEventAsync(ctx, dbg)
		case err := <-done:
			<-mutatorDone
			if err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal("timeout during concurrent breakpoint mutation")
		}
	}
}
