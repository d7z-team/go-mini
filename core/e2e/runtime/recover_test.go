package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestDeferRecover(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
package main

var res = "initial"

func test() {
	defer func() {
		if r := recover(); r != nil {
			res = "recovered: " + string(r)
		}
	}()
	panic("boom")
}

func main() {
	test()
	if res != "recovered: boom" {
		panic("unexpected res: " + res)
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

func TestTryCatchManual(t *testing.T) {
	program := tryProgram(
		map[ast.Ident]ast.Expr{"res": stringLit("initial")},
		nil,
		tryStmt(
			block(call("panic", stringLit("try-boom"))),
			catchStmt("e", assign("res", ident("e"))),
			nil,
		),
		ifStmt(
			binary("Neq", ident("res"), stringLit("try-boom")),
			call("panic", stringLit("failed")),
		),
	)

	runtime := compileASTRuntimeProgram(t, program)
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
