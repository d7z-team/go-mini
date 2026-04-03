package ast_test

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func TestBadExprCallReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 1, C: 1, EL: 4, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 1, C: 12, EL: 4, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 6}},
							Func: &ast.BadExpr{
								BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 4}},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "无法精确推导") {
		t.Fatalf("expected precise inference error, got: %v", err)
	}
}

func TestBadExprMemberReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 1, C: 1, EL: 4, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 1, C: 12, EL: 4, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 10}},
							Func: &ast.ConstRefExpr{
								BaseNode: ast.BaseNode{Meta: "const_ref", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 6}},
								Name:     "print",
							},
							Args: []ast.Expr{
								&ast.MemberExpr{
									BaseNode: ast.BaseNode{Meta: "member", Loc: &ast.Position{L: 2, C: 8, EL: 2, EC: 10}},
									Object: &ast.BadExpr{
										BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 2, C: 8, EL: 2, EC: 8}},
									},
									Property: "X",
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "成员访问对象存在错误") && !strings.Contains(err.Error(), "无法精确推导") {
		t.Fatalf("expected precise member inference error, got: %v", err)
	}
}
