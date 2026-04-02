package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestClosure(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
	package main

	func makeCounter() func() int64 {
		count := 0
		return func() int64 {
			count++
			return count
		}
	}

	func makeAdder(x int64) func(int64) int64 {
		return func(y int64) int64 {
			return x + y
		}
	}

	func main() {
		// 1. Basic closure capture and modification
		counter1 := makeCounter()
		if counter1() != 1 { panic("c1 step 1 failed") }
		if counter1() != 2 { panic("c1 step 2 failed") }

		// 2. Multi-instance isolation
		counter2 := makeCounter()
		if counter2() != 1 { panic("c2 step 1 failed") }
		if counter1() != 3 { panic("c1 step 3 failed") }

		// 3. Parameter capture
		add5 := makeAdder(5)
		if add5(10) != 15 { panic("add5 failed") }
		
		add10 := makeAdder(10)
		if add10(10) != 20 { panic("add10 failed") }

		// 4. Nested closures
		nested := func() func() int64 {
			val := 100
			return func() int64 {
				return func() int64 {
					val += 50
					return val
				}()
			}
		}
		
		nFn := nested()
		if nFn() != 150 { panic("nested step 1 failed") }
		if nFn() != 200 { panic("nested step 2 failed") }
	}
	`

	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err != nil {
		t.Fatalf("failed to execute: %v", err)
	}
}
