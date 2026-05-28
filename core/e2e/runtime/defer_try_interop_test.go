package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFunctionScopedDefersSurviveNestedScopes(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func demo() {
	{
		defer mark("a")
	}
	for i := 0; i < 2; i++ {
		if i == 0 {
			defer mark("b")
		} else {
			defer mark("c")
		}
	}
	mark("z")
}

func main() {
	demo()
	if trace != "zcba" {
		panic("unexpected trace: " + trace)
	}
}
`
	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
