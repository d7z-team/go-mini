package structtest_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/e2e/structtest"
)

func useCalculatorSurface(t *testing.T, executor *engine.MiniExecutor) {
	t.Helper()
	if err := executor.UseSurface(structtest.SurfaceFactory(&structtest.Factory{})); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(structtest.SurfaceCalculator()); err != nil {
		t.Fatal(err)
	}
}

func useTableSurface(t *testing.T, executor *engine.MiniExecutor) {
	t.Helper()
	if err := executor.UseSurface(structtest.SurfaceFactory(&structtest.Factory{})); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(structtest.SurfaceTable()); err != nil {
		t.Fatal(err)
	}
}

func TestStructExport(t *testing.T) {
	executor := engine.NewMiniExecutor()

	useCalculatorSurface(t, executor)
	code := `
	package main
	import "calc"

	func main() {
		// 1. Create a calculator via factory
		c := calc.New(100)
		
		// 2. Call methods on the returned handle
		v1 := c.Add(50)
		if v1 != 150 { panic("Add failed") }
		
		v2 := c.Multiply(10, 20)
		if v2 != 200 { panic("Multiply failed") }
		
		base := c.GetBase()
		if base != 100 { panic("GetBase failed") }
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

func TestAddressOfHostRefRejected(t *testing.T) {
	executor := engine.NewMiniExecutor()
	useCalculatorSurface(t, executor)

	code := `
	package main
	import "calc"

	func main() {
		c := calc.New(100)
		_ = &c
	}
	`
	if _, err := executor.NewRuntimeByGoCode(code); err == nil {
		t.Fatal("expected address-of host reference to be rejected")
	}
}

func TestFFIDefinedObjectAsFunctionParameter(t *testing.T) {
	executor := engine.NewMiniExecutor()

	useCalculatorSurface(t, executor)

	code := `
	package main
	import "calc"

	type Calculator interface {
		Add(Int64) Int64
		GetBase() Int64
	}

	func addViaHelper(c Calculator, delta Int64) Int64 {
		return c.Add(delta)
	}

	func basePlusDelta(c Calculator, delta Int64) Int64 {
		return c.GetBase() + addViaHelper(c, delta)
	}

	func main() {
		c := calc.New(100)

		if addViaHelper(c, 25) != 125 {
			panic("ffi object parameter call failed")
		}

		if basePlusDelta(c, 10) != 210 {
			panic("ffi object nested parameter call failed")
		}
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

func TestFFIDefinedObjectAsPointerTypedFunctionParameter(t *testing.T) {
	executor := engine.NewMiniExecutor()

	useCalculatorSurface(t, executor)

	code := `
	package main
	import "calc"

	func addViaPointer(c *calc.Calculator, delta Int64) Int64 {
		return c.Add(delta)
	}

	func readBaseViaPointer(c *calc.Calculator) Int64 {
		return c.GetBase()
	}

	func main() {
		c := calc.New(100)

		if addViaPointer(c, 15) != 115 {
			panic("ffi pointer-typed parameter call failed")
		}

		if readBaseViaPointer(c) != 100 {
			panic("ffi pointer-typed parameter base read failed")
		}
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

func TestFFIStructMethodGroupedParametersE2E(t *testing.T) {
	executor := engine.NewMiniExecutor()

	useTableSurface(t, executor)

	code := `
	package main
	import "calc"

	func main() {
		tbl := calc.NewTable()
		tbl.SetString(1, 2, "hello")
		if tbl.GetString(1, 2) != "hello" {
			panic("ffi grouped-parameter method call failed")
		}
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
