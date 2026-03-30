package e2e

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core"
)

func TestMathConstants(t *testing.T) {
	mini := engine.NewMiniExecutor()
	mini.InjectStandardLibraries()

	code := `
package main
import "math"

func testPi() float64 {
	return math.Pi
}

func testE() float64 {
	return math.E
}
`
	vm, err := mini.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	ctx := context.Background()

	// Test Pi
	resPi, err := vm.Eval(ctx, "testPi()", nil)
	if err != nil {
		t.Errorf("failed to eval testPi: %v", err)
	}
	if resPi.F64 != 3.14159265358979323846 {
		t.Errorf("expected 3.141592653589793, got %v", resPi.F64)
	}

	// Test E
	resE, err := vm.Eval(ctx, "testE()", nil)
	if err != nil {
		t.Errorf("failed to eval testE: %v", err)
	}
	if resE.F64 != 2.71828182845904523536 {
		t.Errorf("expected 2.718281828459045, got %v", resE.F64)
	}
}
