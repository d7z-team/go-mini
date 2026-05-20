package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func newGoProgram(t *testing.T, code string) *engine.MiniProgram {
	t.Helper()
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	return prog
}

func TestGoRootReturnStopsPendingExecutionContext(t *testing.T) {
	prog := newGoProgram(t, `
package main

import "time"

var done = false

func worker() {
	time.Sleep(200000000)
	done = true
}

func main() {
	go worker()
}
`)
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	done, ok := prog.SharedState().LoadGlobal("done")
	if !ok {
		t.Fatal("missing global done")
	}
	if done == nil || done.Bool {
		t.Fatalf("expected pending execution context to be stopped before setting done, got %#v", done)
	}
}

func TestGoExecutionContextRunsAtYieldPoint(t *testing.T) {
	prog := newGoProgram(t, `
package main

import "time"

var done = false

func worker() {
	done = true
}

func main() {
	go worker()
	time.Sleep(1)
	if !done {
		panic("worker did not run")
	}
}
`)
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoSleepSwitchesExecutionContext(t *testing.T) {
	prog := newGoProgram(t, `
package main

import "time"

var step = 0

func worker() {
	time.Sleep(5000000)
	step = 2
}

func main() {
	go worker()
	step = 1
	time.Sleep(15000000)
	if step != 2 {
		panic("sleep did not resume worker")
	}
}
`)
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoExecutionContextSharesCapturedState(t *testing.T) {
	prog := newGoProgram(t, `
package main

import "time"

func main() {
	x := 1
	go func() {
		x = 2
	}()
	time.Sleep(1)
	if x != 2 {
		panic("go execution context did not share captured state")
	}
}
`)
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoExecutionContextStructArgumentUsesValueSemantics(t *testing.T) {
	prog := newGoProgram(t, `
package main

import "time"

type Box struct {
	Value int
}

func worker(b Box) {
	b.Value = 2
}

func main() {
	b := Box{Value: 1}
	go worker(b)
	time.Sleep(1)
	if b.Value != 1 {
		panic("go execution context argument mutated caller struct")
	}
}
`)
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoExecutionContextPanicFailsProgram(t *testing.T) {
	prog := newGoProgram(t, `
package main

import "time"

func fail() {
	panic("boom")
}

func main() {
	go fail()
	time.Sleep(1)
}
`)
	err := prog.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected child execution context panic, got %v", err)
	}
}
