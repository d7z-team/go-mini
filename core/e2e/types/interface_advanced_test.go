package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestNamedInterface(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main
	
	type Describer interface {
		Describe() String
	}

	func printDesc(d Describer) String {
		return d.Describe()
	}

	func main() {
		obj := make(map[String]Any)
		obj["Describe"] = func() String { return "I am a map" }
		
		res := printDesc(obj)
		if res != "I am a map" {
			panic("named interface call failed: " + res)
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

func TestTypeAssertion(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main
	
	type Reader interface {
		Read() String
	}

	func main() {
		var a Any = make(map[String]Any)
		a["Read"] = func() String { return "content" }
		
		r := a.(Reader)
		if r.Read() != "content" {
			panic("assertion failed")
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

func TestTypeAssertionFailure(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main
	
	type Reader interface {
		Read() String
	}

	func main() {
		var a Any = 123
		r := a.(Reader)
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected assertion error, but got nil")
	}
}
