package runtime

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func TestNewExecutorFromPreparedRequiresPreparedProgram(t *testing.T) {
	_, err := NewExecutorFromPrepared(&ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "prepared-test"},
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Main:       []ast.Stmt{},
	}, nil)
	if err == nil {
		t.Fatal("expected missing prepared program error")
	}
	if !strings.Contains(err.Error(), "missing prepared program") {
		t.Fatalf("unexpected error: %v", err)
	}
}
