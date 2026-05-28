package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
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

func TestTryCatchKeepsFunctionDefer(t *testing.T) {
	program := tryProgram(
		map[ast.Ident]ast.Expr{"res": stringLit("")},
		map[ast.Ident]*ast.FunctionStmt{
			"appendText": appendTextFunction(),
			"demo": fn("demo", nil,
				deferCall("appendText", stringLit(":defer")),
				tryStmt(
					block(call("panic", stringLit("boom"))),
					catchStmt("e", call("appendText", stringLit(":catch=boom"))),
					nil,
				),
			),
		},
		call("demo"),
		unexpectedResultGuard(":catch=boom:defer"),
	)

	runtime := compileASTRuntimeProgram(t, program)
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTryFinallyRunsBeforeRecoveringFunctionDefer(t *testing.T) {
	program := tryProgram(
		map[ast.Ident]ast.Expr{"res": stringLit("")},
		map[ast.Ident]*ast.FunctionStmt{
			"appendText": appendTextFunction(),
			"captureRecover": fn("captureRecover", nil,
				ifStmt(
					binary("Neq", call("recover"), ident("nil")),
					call("appendText", stringLit(":recovered")),
				),
			),
			"demo": fn("demo", nil,
				deferCall("captureRecover"),
				tryStmt(
					block(call("panic", stringLit("boom"))),
					nil,
					block(call("appendText", stringLit(":finally"))),
				),
			),
		},
		call("demo"),
		unexpectedResultGuard(":finally:recovered"),
	)

	runtime := compileASTRuntimeProgram(t, program)
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
