package ast

import (
	"strings"
	"testing"
)

func TestReturnAnalyzerTreatsPanicAsTerminating(t *testing.T) {
	program := &ProgramStmt{
		Package:   "main",
		Constants: map[string]string{},
		Variables: map[Ident]Expr{},
		Types:     map[Ident]GoMiniType{},
		Structs:   map[Ident]*StructStmt{},
		Interfaces: map[Ident]*InterfaceStmt{},
		Functions: map[Ident]*FunctionStmt{
			"fail": {
				Name: "fail",
				FunctionType: FunctionType{
					Return: "String",
				},
				Body: &BlockStmt{
					Children: []Stmt{
						&CallExprStmt{
							Func: &ConstRefExpr{Name: "panic"},
							Args: []Expr{
								&LiteralExpr{
									BaseNode: BaseNode{Type: "String"},
									Value:    "boom",
								},
							},
						},
					},
				},
			},
		},
		Main: []Stmt{},
	}

	ctx, err := NewValidator(program, map[Ident]GoMiniType{
		"panic": "function(Any) Void",
	}, nil, false)
	if err != nil {
		t.Fatalf("validator init failed: %v", err)
	}

	if err := program.Check(NewSemanticContext(ctx)); err != nil {
		t.Fatalf("panic-only function should satisfy return analysis: %v", err)
	}
}

func TestReturnAnalyzerStillRejectsMissingReturn(t *testing.T) {
	program := &ProgramStmt{
		Package:   "main",
		Constants: map[string]string{},
		Variables: map[Ident]Expr{},
		Types:     map[Ident]GoMiniType{},
		Structs:   map[Ident]*StructStmt{},
		Interfaces: map[Ident]*InterfaceStmt{},
		Functions: map[Ident]*FunctionStmt{
			"fail": {
				Name: "fail",
				FunctionType: FunctionType{
					Return: "String",
				},
				Body: &BlockStmt{},
			},
		},
		Main: []Stmt{},
	}

	ctx, err := NewValidator(program, nil, nil, false)
	if err != nil {
		t.Fatalf("validator init failed: %v", err)
	}

	err = program.Check(NewSemanticContext(ctx))
	if err == nil {
		t.Fatal("expected missing return validation failure")
	}
	if !strings.Contains(err.Error(), "缺少返回语句") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
