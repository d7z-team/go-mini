package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestShadowingInDefine(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func getValues() (Int64, String) {
		return 42, ""
	}
	
	func main() {
		a, err := getValues()
		if err != "" { panic(err) }
		
		// This should be allowed in Go: 'err' is reassigned, 'b' is new.
		b, err := getValues()
		if err != "" { panic(err) }
		
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
