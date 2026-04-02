package structtest_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/e2e/structtest"
)

func TestStructExport(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	registry := executor.HandleRegistry()

	// Register the factory as a module
	structtest.RegisterFactory(executor, &structtest.Factory{}, registry)
	// Register the Calculator methods
	// Note: We don't need a specific instance of Calculator to register its methods,
	// because the methods themselves will be called on instances returned by the factory.
	structtest.RegisterCalculator(executor, registry)
	code := `
	package main
	import "calc"
	import "fmt"

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
		
		fmt.Println("Struct Export Verified: ", v1, v2, base)
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

func TestFFIDefinedObjectAsFunctionParameter(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	registry := executor.HandleRegistry()
	structtest.RegisterFactory(executor, &structtest.Factory{}, registry)
	structtest.RegisterCalculator(executor, registry)

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
	executor.InjectStandardLibraries()

	registry := executor.HandleRegistry()
	structtest.RegisterFactory(executor, &structtest.Factory{}, registry)
	structtest.RegisterCalculator(executor, registry)

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
