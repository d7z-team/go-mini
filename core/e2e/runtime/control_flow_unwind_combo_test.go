package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestSwitchContinuePropagatesToOuterLoop(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func main() {
	sum := 0
	for i := 0; i < 4; i++ {
		switch i {
		case 1:
			mark("c")
			continue
		case 3:
			mark("d")
		}
		sum = sum + i
		mark("x")
	}
	if sum != 5 {
		panic("sum mismatch")
	}
	if trace != "xcxdx" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionDeferSurvivesLoopControlFlow(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func demo() {
	for i := 0; i < 4; i++ {
		if i == 0 {
			defer mark("0")
		} else if i == 1 {
			defer mark("1")
		} else if i == 2 {
			defer mark("2")
		} else {
			defer mark("3")
		}
		if i == 1 {
			continue
		}
		if i == 3 {
			break
		}
		mark("x")
	}
}

func main() {
	demo()
	if trace != "xx3210" {
		panic("trace mismatch: " + trace)
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
