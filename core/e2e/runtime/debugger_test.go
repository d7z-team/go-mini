package tests

import (
	"context"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/debugger"
)

func loadInt64Global(t *testing.T, prog *engine.ExecutableProgram, name string) int64 {
	t.Helper()
	value, ok := prog.SharedState().LoadGlobal(name)
	if !ok || value == nil {
		t.Fatalf("missing global %s", name)
	}
	return value.I64
}

func TestDebuggerBasicBreakAndStep(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	select {
	case event := <-dbg.Events():
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
		dbg.StepInto()
	case <-ctx.Done():
		t.Fatal("timeout waiting for breakpoint at line 5")
	}

	for {
		select {
		case event := <-dbg.Events():
			if event.Loc.L == 6 {
				if event.Variables["b"] != "20" {
					t.Fatalf("expected b=20, got %v", event.Variables["b"])
				}
				dbg.Continue()
				goto WAIT_DONE
			}
			if event.Loc.L == 5 {
				dbg.StepInto()
				continue
			}
			t.Fatalf("unexpected step to line %d", event.Loc.L)
		case <-ctx.Done():
			t.Fatal("timeout waiting for step to line 6")
		}
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
	testExecutor := engine.NewMiniExecutor()

	sourceSnippet := `
		x := 100 // Line 2
		y := 200 // Line 3
		z := x * y
	`

	dbg := debugger.NewSession()
	dbg.SetStepping(true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	done := make(chan error, 1)
	go func() {
		done <- testExecutor.Execute(ctx, sourceSnippet, nil)
	}()

	linesSeen := []int{}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case event := <-dbg.Events():
			linesSeen = append(linesSeen, event.Loc.L)
			if event.Loc.L == 3 && event.Variables["x"] != "100" {
				t.Fatalf("expected x=100, got %v", event.Variables["x"])
			}
			dbg.StepInto()
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

func TestDebuggerLoopExecution(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	expectedI := []string{"1", "2", "3"}
	expectedSum := []string{"0", "1", "3"}
	for loopCount := 0; loopCount < 3; loopCount++ {
		select {
		case event := <-dbg.Events():
			if event.Loc.L != 6 {
				t.Fatalf("expected break at line 6, got %d", event.Loc.L)
			}
			if actual := event.Variables["i"]; actual != expectedI[loopCount] {
				t.Errorf("loop %d: expected i=%s, got %s", loopCount, expectedI[loopCount], actual)
			}
			if actual := event.Variables["sum"]; actual != expectedSum[loopCount] {
				t.Errorf("loop %d: expected sum=%s, got %s", loopCount, expectedSum[loopCount], actual)
			}
			dbg.Continue()
		case <-ctx.Done():
			t.Fatalf("timeout waiting for breakpoint in loop %d", loopCount)
		}
	}

	select {
	case <-dbg.Events():
		t.Fatal("breakpoint hit more times than expected")
	case <-time.After(100 * time.Millisecond):
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerAnytimePause(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	dbg.RequestPause()
	select {
	case <-dbg.Events():
		dbg.Continue()
	case <-ctx.Done():
		t.Fatal("timeout waiting for anytime pause")
	}

	time.Sleep(20 * time.Millisecond)
	dbg.RequestPause()
	select {
	case <-dbg.Events():
		dbg.Continue()
	case <-ctx.Done():
		t.Fatal("timeout waiting for second anytime pause")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerContextBreakpointHitsChildContext(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	select {
	case event := <-dbg.Events():
		if event.Loc.L != 5 {
			t.Fatalf("expected child execution context breakpoint at line 5, got %d", event.Loc.L)
		}
		if event.ExecutionContextID <= 1 {
			t.Fatalf("expected child execution context id greater than root, got %d", event.ExecutionContextID)
		}
		dbg.Continue()
	case <-ctx.Done():
		t.Fatal("timeout waiting for child execution context breakpoint")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerContextBreakpointHitsMultipleContexts(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	hits := 0
	contextHits := make(map[uint32]bool)
	for {
		select {
		case event := <-dbg.Events():
			if event.Loc.L != 5 {
				t.Fatalf("expected child execution context breakpoint at line 5, got %d", event.Loc.L)
			}
			if event.ExecutionContextID <= 1 {
				t.Fatalf("expected child execution context id greater than root, got %d", event.ExecutionContextID)
			}
			hits++
			contextHits[event.ExecutionContextID] = true
			dbg.Continue()
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
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	select {
	case event := <-dbg.Events():
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
		dbg.Continue()
	case <-ctx.Done():
		t.Fatal("timeout waiting for child execution context breakpoint")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDebuggerRemoveBreakpointAfterHitPreventsLaterHits(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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

	done := make(chan error, 1)
	go func() {
		done <- testProgram.Execute(ctx)
	}()

	select {
	case event := <-dbg.Events():
		if event.Loc.L != 6 {
			t.Fatalf("expected first breakpoint at line 6, got %d", event.Loc.L)
		}
		dbg.RemoveBreakpoint(6)
		dbg.Continue()
	case <-ctx.Done():
		t.Fatal("timeout waiting for first breakpoint")
	}

	select {
	case event := <-dbg.Events():
		t.Fatalf("removed breakpoint should not pause again, got line %d", event.Loc.L)
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for completion after breakpoint removal")
	}
}

func TestDebuggerConcurrentBreakpointMutation(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
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
	go func() {
		err := testProgram.Execute(ctx)
		close(stopMutating)
		done <- err
	}()

	for {
		select {
		case <-dbg.Events():
			dbg.Continue()
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
