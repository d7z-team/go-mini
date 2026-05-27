package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestLoopClosureCaptureUsesPerIterationBindings(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
		package main
		func main() {
			fns := make([]func() int64, 3)
			for i := 0; i < 3; i++ {
				fns[i] = func() int64 { return i }
			}

			v0 := fns[0]()
			if v0 != 0 { panic("loop capture 0 failed") }
			v1 := fns[1]()
			if v1 != 1 { panic("loop capture 1 failed") }
			v2 := fns[2]()
			if v2 != 2 { panic("loop capture 2 failed") }
		}
		`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("loop capture test failed: %v", err)
	}
}

func TestRangeLoopClosureCaptureUsesPerIterationBindings(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
		package main
		func main() {
			arr := []int64{10, 20, 30}
			fns := make([]func() int64, 3)
			for i, v := range arr {
				fns[i] = func() int64 { return v }
			}

			v0 := fns[0]()
			if v0 != 10 { panic("range loop capture 0 failed") }
			v1 := fns[1]()
			if v1 != 20 { panic("range loop capture 1 failed") }
			v2 := fns[2]()
			if v2 != 30 { panic("range loop capture 2 failed") }
		}
		`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("range loop capture test failed: %v", err)
	}
}
