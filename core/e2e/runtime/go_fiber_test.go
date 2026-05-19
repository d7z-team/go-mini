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

func TestGoRootReturnStopsPendingFiber(t *testing.T) {
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
		t.Fatalf("expected pending fiber to be stopped before setting done, got %#v", done)
	}
}

func TestGoFiberRunsAtYieldPoint(t *testing.T) {
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

func TestGoFiberSleepSwitchesContext(t *testing.T) {
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

func TestGoFiberSharesCapturedState(t *testing.T) {
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
		panic("go fiber did not share captured state")
	}
}
`)
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoFiberPanicFailsProgram(t *testing.T) {
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
		t.Fatalf("expected child fiber panic, got %v", err)
	}
}
