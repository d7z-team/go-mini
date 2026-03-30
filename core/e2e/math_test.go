package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestMathLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	import "math"

	func main() {
		// Test basic functions
		if math.Abs(-1.0) != 1.0 { panic("Abs failed") }
		if math.Ceil(1.1) != 2.0 { panic("Ceil failed") }
		if math.Floor(1.9) != 1.0 { panic("Floor failed") }
		if math.Round(1.5) != 2.0 { panic("Round failed") }
		if math.Sqrt(4.0) != 2.0 { panic("Sqrt failed") }
		if math.Pow(2.0, 3.0) != 8.0 { panic("Pow failed") }
		
		// Test Min/Max
		if math.Min(1.0, 2.0) != 1.0 { panic("Min failed") }
		if math.Max(1.0, 2.0) != 2.0 { panic("Max failed") }
		
		// Test NaN/Inf
		nan := math.NaN()
		if !math.IsNaN(nan) { panic("IsNaN failed") }
		
		inf := math.Inf(1)
		if !math.IsInf(inf, 1) { panic("IsInf failed") }
		
		// Test Constants
		pi := math.Pi
		if pi < 3.14 || pi > 3.15 { panic("Pi failed") }
		
		e := math.E
		if e < 2.71 || e > 2.72 { panic("E failed") }
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
