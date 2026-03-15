package ast

import (
	"fmt"
	"testing"
)

func TestValidationPipeline(t *testing.T) {
	// 构造一个简单的 1 + 2 的表达式
	expr := &BinaryExpr{
		BaseNode: BaseNode{
			ID:      "test_bin",
			Meta:    "binary",
			Message: "test",
		},
		Operator: "Plus",
		Left: &LiteralExpr{
			BaseNode: BaseNode{
				ID:   "test_l1",
				Meta: "literal",
				Type: "Int64",
			},
			Value: "1",
		},
		Right: &LiteralExpr{
			BaseNode: BaseNode{
				ID:   "test_l2",
				Meta: "literal",
				Type: "Int64",
			},
			Value: "2",
		},
	}

	program := &ProgramStmt{
		Constants: make(map[string]string),
		Main: []Stmt{
			&AssignmentStmt{
				BaseNode: BaseNode{
					ID: "test_assign",
				},
				Variable: "x",
				Value:    expr,
			},
		},
		Variables: map[Ident]Expr{
			"x": &LiteralExpr{
				BaseNode: BaseNode{
					Type: "Int64",
				},
				Value: "0",
			},
		},
	}

	ctx, err := NewValidator(program)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// 运行完整流水线
	semCtx := NewSemanticContext(ctx)
	err = program.Check(semCtx)
	if err != nil {
		for _, log := range ctx.Logs() {
			t.Errorf("Validation log: %s", log.Message)
		}
		t.Fatalf("Validation failed: %v", err)
	}
	optCtx := NewOptimizeContext(ctx)
	optimized := program.Optimize(optCtx)

	optProg := optimized.(*ProgramStmt)
	// 在目前的实现中，BinaryExpr 的 Validate 已经包含了常量折叠
	// 我们检查赋值语句的值是否被折叠为 "3"
	assign := optProg.Main[0].(*AssignmentStmt)
	lit, ok := assign.Value.(*LiteralExpr)
	if !ok {
		t.Fatalf("Expression was not folded to LiteralExpr, got %T", assign.Value)
	}

	if lit.Value != "3" {
		t.Errorf("Expected folded value '3', got '%s'", lit.Value)
	}

	fmt.Printf("Pipeline test passed, folded value: %s\n", lit.Value)
}
