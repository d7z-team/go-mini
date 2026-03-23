package ast

import (
	"errors"
)

// IncDecStmt 表示自增自减语句
type IncDecStmt struct {
	BaseNode
	Operand  Expr  `json:"operand"`
	Operator Ident `json:"operator"`
}

func (i *IncDecStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *IncDecStmt) stmtNode()          {}

func (i *IncDecStmt) Check(ctx *SemanticContext) error {
	i.Type = "Void"
	if i.Operand == nil {
		return errors.New("inc/dec 语句缺少操作数")
	}
	if err := i.Operand.Check(ctx); err != nil {
		return err
	}
	// 验证操作数是否为数值类型
	oType := i.Operand.GetBase().Type
	if oType != "Int64" && oType != "Float64" && oType != "Int" {
		return errors.New("inc/dec 语句的操作数必须是数值类型")
	}
	return nil
}

func (i *IncDecStmt) Optimize(ctx *OptimizeContext) Node {
	i.Operand = i.Operand.Optimize(ctx).(Expr)
	return i
}

// ExpressionStmt 表示一个纯表达式语句
type ExpressionStmt struct {
	BaseNode
	X Expr `json:"x"`
}

func (e *ExpressionStmt) GetBase() *BaseNode { return &e.BaseNode }
func (e *ExpressionStmt) stmtNode()          {}

func (e *ExpressionStmt) Check(ctx *SemanticContext) error {
	if e.X == nil {
		return errors.New("expression statement is missing expression")
	}
	return e.X.Check(ctx)
}

func (e *ExpressionStmt) Optimize(ctx *OptimizeContext) Node {
	if e.X != nil {
		e.X = e.X.Optimize(ctx).(Expr)
	}
	return e
}

// 注意: SwitchStmt 和 RangeStmt 已迁移至 ast_stmt.go
