package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestTypeAssertOk(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		var x Any = 42
		
		// Case 1: Success
		v, ok := x.(Int64)
		if !ok { panic("ok should be true") }
		if v != 42 { panic("v should be 42") }
		
		// Case 2: Failure
		v2, ok2 := x.(String)
		if ok2 { panic("ok2 should be false") }
		if v2 != nil { panic("v2 should be nil") }
		
		// Case 3: Single assign still fails
		// defer func() { if recover() == nil { panic("should panic") } }()
		// v3 := x.(String)
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
