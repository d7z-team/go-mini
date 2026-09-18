package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestShadowingInDefine(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main
	
	func getValues() (Int64, error) {
		return 42, nil
	}
	
	func main() {
		a, err := getValues()
		if err != nil { panic(err) }
		
		// This should be allowed in Go: 'err' is reassigned, 'b' is new.
		b, err := getValues()
		if err != nil { panic(err) }
		
		if a != 42 || b != 42 {
			panic("wrong values")
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

func TestForInitShortDeclShadowsOuterVariable(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main

	func main() {
		i := 100
		sum := 0
		for i := 0; i < 4; i++ {
			sum = sum + i
		}
		if sum != 6 {
			panic("loop sum mismatch")
		}
		if i != 100 {
			panic("outer variable should not be overwritten by for init")
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

func TestRangeShortDeclShadowsOuterVariable(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main

	func main() {
		i := 100
		sum := 0
		for i := range []Int64{10, 20, 30} {
			sum = sum + i
		}
		if sum != 3 {
			panic("range index sum mismatch")
		}
		if i != 100 {
			panic("outer variable should not be overwritten by range short declaration")
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

func TestRangeAssignUsesExistingVariables(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main

	func main() {
		i := -1
		v := -1
		for i, v = range []Int64{10, 20, 30} {
		}
		if i != 2 {
			panic("range index assignment failed")
		}
		if v != 30 {
			panic("range value assignment failed")
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
