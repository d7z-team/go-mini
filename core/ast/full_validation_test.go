package ast

import (
	"testing"
)

func TestFullValidation(t *testing.T) {
	// Task 3: IfStmt + LiteralExpr(false) + CallExprStmt(undefined_function)
	// ProgramStmt.Check(semCtx) must return an error.

	program := &ProgramStmt{
		BaseNode: BaseNode{ID: "root"},
		Main: []Stmt{
			&IfStmt{
				BaseNode: BaseNode{ID: "if_stmt"},
				Cond: &LiteralExpr{
					BaseNode: BaseNode{ID: "false_lit", Type: "Bool"},
					Value:    "false",
				},
				Body: &BlockStmt{
					BaseNode: BaseNode{ID: "body"},
					Children: []Stmt{
						&CallExprStmt{
							BaseNode: BaseNode{ID: "undefined_call"},
							Func: &IdentifierExpr{
								BaseNode: BaseNode{ID: "undefined_func_ident"},
								Name:     "undefined_function",
							},
							Args: []Expr{},
						},
					},
				},
			},
		},
	}

	semCtx, err := NewValidator(program)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// We need to use Check on the ProgramStmt
	semanticCtx := NewSemanticContext(semCtx)
	err = program.Check(semanticCtx)

	if err == nil {
		t.Errorf("Expected error for undefined function in dead code, but got nil")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}
