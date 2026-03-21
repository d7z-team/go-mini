package runtime

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func TestScope(t *testing.T) {
	ctx := &StackContext{
		Stack: &Stack{
			MemoryPtr: make(map[IdentVar]*Var),
			Scope:     "global",
		},
	}

	// Add variable
	v := NewInt(100)
	ctx.AddVariable("a", v)

	// Load variable
	got, err := ctx.Load("a")
	if err != nil || got.I64 != 100 {
		t.Errorf("Load failed: %v, %v", got, err)
	}

	// Nested scope
	ctx.WithScope("local", func(c *StackContext) {
		c.AddVariable("b", NewInt(200))

		// Access parent
		gotA, _ := c.Load("a")
		if gotA.I64 != 100 {
			t.Error("failed to access parent scope")
		}

		// Shadowing (if allowed, here we just test isolation)
		c.AddVariable("a", NewInt(300))
		gotShadow, _ := c.Load("a")
		if gotShadow.I64 != 300 {
			t.Error("shadowing failed")
		}
	})

	// Check isolation
	gotBack, _ := ctx.Load("a")
	if gotBack.I64 != 100 {
		t.Error("scope isolation failed")
	}

	_, err = ctx.Load("b")
	if err == nil {
		t.Error("should not access child scope variable")
	}
}

type IdentVar = string // Alias for convenience in test

func TestExecutorBasic(t *testing.T) {
	// Program: x = 10; y = 20; res = x + y * 2;
	main := []ast.Stmt{
		&ast.GenDeclStmt{Name: "x", Kind: "Int64"},
		&ast.AssignmentStmt{LHS: &ast.IdentifierExpr{Name: "x"}, Value: &ast.LiteralExpr{Value: "10", BaseNode: ast.BaseNode{Type: "Int64"}}},
		&ast.GenDeclStmt{Name: "y", Kind: "Int64"},
		&ast.AssignmentStmt{LHS: &ast.IdentifierExpr{Name: "y"}, Value: &ast.LiteralExpr{Value: "20", BaseNode: ast.BaseNode{Type: "Int64"}}},
		&ast.GenDeclStmt{Name: "res", Kind: "Int64"},
		&ast.AssignmentStmt{
			LHS: &ast.IdentifierExpr{Name: "res"},
			Value: &ast.BinaryExpr{
				Operator: "Plus",
				Left:     &ast.IdentifierExpr{Name: "x"},
				Right: &ast.BinaryExpr{
					Operator: "Mult",
					Left:     &ast.IdentifierExpr{Name: "y"},
					Right:    &ast.LiteralExpr{Value: "2", BaseNode: ast.BaseNode{Type: "Int64"}},
				},
			},
		},
	}

	prog := &ast.ProgramStmt{
		Main:      main,
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
		Constants: make(map[string]string),
	}

	exec, _ := NewExecutor(prog)
	err := exec.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	res, _ := exec.lastSession.Load("res")
	if res == nil || res.I64 != 50 {
		t.Errorf("Calculation failed: got %v, want 50", res)
	}
}

func TestControlFlowIfFor(t *testing.T) {
	// Program:
	// sum = 0
	// for i = 0; i < 5; i++ { sum = sum + i }
	// if sum == 10 { ok = true } else { ok = false }

	main := []ast.Stmt{
		&ast.GenDeclStmt{Name: "sum", Kind: "Int64"},
		&ast.AssignmentStmt{LHS: &ast.IdentifierExpr{Name: "sum"}, Value: &ast.LiteralExpr{Value: "0", BaseNode: ast.BaseNode{Type: "Int64"}}},
		&ast.ForStmt{
			Init:   &ast.AssignmentStmt{LHS: &ast.IdentifierExpr{Name: "i"}, Value: &ast.LiteralExpr{Value: "0", BaseNode: ast.BaseNode{Type: "Int64"}}},
			Cond:   &ast.BinaryExpr{Operator: "Lt", Left: &ast.IdentifierExpr{Name: "i"}, Right: &ast.LiteralExpr{Value: "5", BaseNode: ast.BaseNode{Type: "Int64"}}},
			Update: &ast.IncDecStmt{Operand: &ast.IdentifierExpr{Name: "i"}, Operator: "++"},
			Body: &ast.BlockStmt{Children: []ast.Stmt{
				&ast.AssignmentStmt{
					LHS:   &ast.IdentifierExpr{Name: "sum"},
					Value: &ast.BinaryExpr{Operator: "Plus", Left: &ast.IdentifierExpr{Name: "sum"}, Right: &ast.IdentifierExpr{Name: "i"}},
				},
			}},
		},
		&ast.GenDeclStmt{Name: "ok", Kind: "Bool"},
		&ast.IfStmt{
			Cond: &ast.BinaryExpr{Operator: "Eq", Left: &ast.IdentifierExpr{Name: "sum"}, Right: &ast.LiteralExpr{Value: "10", BaseNode: ast.BaseNode{Type: "Int64"}}},
			Body: &ast.BlockStmt{Children: []ast.Stmt{
				&ast.AssignmentStmt{LHS: &ast.IdentifierExpr{Name: "ok"}, Value: &ast.LiteralExpr{Value: "true", BaseNode: ast.BaseNode{Type: "Bool"}}},
			}},
			ElseBody: &ast.BlockStmt{Children: []ast.Stmt{
				&ast.AssignmentStmt{LHS: &ast.IdentifierExpr{Name: "ok"}, Value: &ast.LiteralExpr{Value: "false", BaseNode: ast.BaseNode{Type: "Bool"}}},
			}},
		},
	}

	prog := &ast.ProgramStmt{
		Main:      main,
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
		Constants: make(map[string]string),
	}

	exec, _ := NewExecutor(prog)
	err := exec.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	okVar, _ := exec.lastSession.Load("ok")
	if okVar == nil || !okVar.Bool {
		sumVar, _ := exec.lastSession.Load("sum")
		t.Errorf("Control flow failed: sum=%v, ok=%v", sumVar, okVar)
	}
}
