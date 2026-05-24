package tests

import (
	"context"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func TestTryCatchDeepPanic(t *testing.T) {
	program := tryProgram(
		map[ast.Ident]ast.Expr{"res": stringLit("initial")},
		map[ast.Ident]*ast.FunctionStmt{
			"doPanic": fn("doPanic", nil, call("panic", stringLit("try-boom"))),
		},
		tryStmt(
			block(call("doPanic")),
			catchStmt("e", assign("res", ident("e"))),
			nil,
		),
		ifStmt(
			binary("Neq", call("recover"), ident("nil")),
			call("panic", stringLit("stale-panic-var-leaked")),
		),
	)

	runtime := compileASTRuntimeProgram(t, program)
	err := runtime.Execute(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), "stale-panic-var-leaked") {
			t.Fatalf("TryStmt leaked panic state")
		}
		t.Fatalf("expected nil error, got: %v", err)
	}
}
