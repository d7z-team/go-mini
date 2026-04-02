package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFunctionDeclarationOrderAndCalls(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func main() {
		if compose(2, 3) != 25 {
			panic("compose failed")
		}
		if declaredLater(10) != 15 {
			panic("declared later failed")
		}
	}

	func compose(a Int64, b Int64) Int64 {
		return square(add(a, b))
	}

	func add(a Int64, b Int64) Int64 {
		return a + b
	}

	func square(v Int64) Int64 {
		return v * v
	}

	func declaredLater(v Int64) Int64 {
		return v + 5
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

func TestFunctionValuesAndHigherOrderCalls(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func applyTwice(fn func(Int64) Int64, value Int64) Int64 {
		return fn(fn(value))
	}

	func choose(op String) func(Int64, Int64) Int64 {
		if op == "mul" {
			return func(a Int64, b Int64) Int64 {
				return a * b
			}
		}
		return func(a Int64, b Int64) Int64 {
			return a + b
		}
	}

	func main() {
		fn := func(v Int64) Int64 {
			return v + 1
		}
		if applyTwice(fn, 3) != 5 {
			panic("applyTwice failed")
		}

		selected := choose("mul")
		if selected(4, 5) != 20 {
			panic("selected multiply failed")
		}

		selected = choose("add")
		if selected(4, 5) != 9 {
			panic("selected add failed")
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

func TestRecursiveAndMutualFunctionCalls(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func factorial(n Int64) Int64 {
		if n <= 1 {
			return 1
		}
		return n * factorial(n - 1)
	}

	func isEven(n Int64) Bool {
		if n == 0 {
			return true
		}
		return isOdd(n - 1)
	}

	func isOdd(n Int64) Bool {
		if n == 0 {
			return false
		}
		return isEven(n - 1)
	}

	func main() {
		if factorial(5) != 120 {
			panic("factorial failed")
		}
		if !isEven(10) {
			panic("isEven failed")
		}
		if !isOdd(9) {
			panic("isOdd failed")
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

func TestFunctionParametersWithStructAndNumericValues(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	type Point struct {
		X Int64
		Y Int64
	}

	func sumPoint(point Point, offset Int64) Int64 {
		return point.X + point.Y + offset
	}

	func amplify(base Int64, factor Int64, extra Int64) Int64 {
		return base*factor + extra
	}

	func main() {
		point := Point{X: 4, Y: 5}
		if sumPoint(point, 3) != 12 {
			panic("struct parameter failed")
		}

		first := amplify(6, 7, 2)
		if first != 44 {
			panic("numeric parameter failed")
		}

		second := amplify(sumPoint(point, 1), 2, 3)
		if second != 23 {
			panic("nested numeric call failed")
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
