package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestNamedType(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type MyInt int64
	type MyString string
	
	func main() {
		var a MyInt = 10
		var b MyString = "hello"
		
		if a != 10 {
			panic("MyInt failed")
		}
		if b != "hello" {
			panic("MyString failed")
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

func TestNamedTypeRecursive(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type ID int64
	type UserID ID
	
	func main() {
		var uid UserID = 100
		if uid != 100 {
			panic("Recursive named type failed")
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
