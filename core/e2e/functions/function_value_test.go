package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestNamedFunctionAsValue(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func increment(x Int64) Int64 {
		return x + 1
	}

	func main() {
		fn := increment
		if fn(10) != 11 {
			panic("named function as value failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestNamedFunctionAsArgument(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func increment(x Int64) Int64 {
		return x + 1
	}

	func apply(fn func(Int64) Int64, val Int64) Int64 {
		return fn(val)
	}

	func main() {
		if apply(increment, 20) != 21 {
			panic("named function as argument failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestNamedFunctionInClosure(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func getBase() Int64 {
		return 100
	}

	func makeAdder() func(Int64) Int64 {
		baseFn := getBase
		return func(x Int64) Int64 {
			return baseFn() + x
		}
	}

	func main() {
		adder := makeAdder()
		if adder(5) != 105 {
			panic("named function in closure failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
