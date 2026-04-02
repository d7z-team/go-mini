package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestIfScopeIsolation(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		v := 10
		if v := 20; v == 20 {
			if v != 20 { panic("inner v should be 20") }
		}
		if v != 10 {
			panic("outer v should still be 10, but got: " + string(v))
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

func TestSwitchScopeIsolation(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		v := 10
		switch v := 20; v {
		case 20:
			if v != 20 { panic("inner v should be 20") }
		}
		if v != 10 {
			panic("outer v should still be 10")
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
