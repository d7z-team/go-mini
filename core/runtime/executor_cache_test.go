package runtime

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestExecutorHotPathUsesPreparedProgramCaches(t *testing.T) {
	converter := ffigo.NewGoToASTConverter()

	assignStmts, err := converter.ConvertStmtsSource(`counter = 2`)
	if err != nil {
		t.Fatalf("convert main stmts failed: %v", err)
	}
	bodyStmts, err := converter.ConvertStmtsSource(`return n + 1`)
	if err != nil {
		t.Fatalf("convert function body failed: %v", err)
	}
	callExpr, err := converter.ConvertExprSource(`inc(41)`)
	if err != nil {
		t.Fatalf("convert call expr failed: %v", err)
	}
	prog := &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "cache-test"},
		Constants: map[string]string{"Answer": "42"},
		Variables: map[ast.Ident]ast.Expr{
			"counter": &ast.LiteralExpr{
				BaseNode: ast.BaseNode{Type: "Int64"},
				Value:    "1",
			},
		},
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
		Main:      assignStmts,
	}
	prog.Functions["inc"] = &ast.FunctionStmt{
		BaseNode: ast.BaseNode{ID: "fn-inc"},
		Name:     "inc",
		FunctionType: ast.FunctionType{
			Params: []ast.FunctionParam{{Name: "n", Type: "Int64"}},
			Return: "Int64",
		},
		Body: &ast.BlockStmt{Children: bodyStmts, Inner: true},
	}

	exec := newExecutor(t, prog)
	exec.program = nil

	session := exec.NewSession(context.Background(), "global")
	if err := exec.InitializeSession(session, nil, false); err != nil {
		t.Fatalf("initialize session failed: %v", err)
	}

	counter, err := session.Load("counter")
	if err != nil {
		t.Fatalf("load counter failed: %v", err)
	}
	if counter.I64 != 2 {
		t.Fatalf("unexpected counter value: %#v", counter)
	}

	result, err := exec.ExecExpr(session, callExpr)
	if err != nil {
		t.Fatalf("exec call expr failed: %v", err)
	}
	if result == nil || result.VType != TypeInt || result.I64 != 42 {
		t.Fatalf("unexpected function result: %#v", result)
	}

	answer, err := exec.ExecExpr(session, &ast.ConstRefExpr{Name: "Answer"})
	if err != nil {
		t.Fatalf("exec const expr failed: %v", err)
	}
	if answer == nil || answer.VType != TypeInt || answer.I64 != 42 {
		t.Fatalf("unexpected const result: %#v", answer)
	}
}
