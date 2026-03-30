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
	ctx = ctx.WithNode(i)
	i.Type = "Void"
	if i.Operand == nil {
		err := errors.New("inc/dec 语句缺少操作数")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if err := i.Operand.Check(ctx.WithNode(i.Operand)); err != nil {
		return err
	}
	// 验证操作数是否为数值类型
	oType := i.Operand.GetBase().Type
	if oType != "Int64" && oType != "Float64" && oType != "Int" {
		err := errors.New("inc/dec 语句的操作数必须是数值类型")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	return nil
}

func (i *IncDecStmt) Optimize(ctx *OptimizeContext) Node {
	if i.Operand != nil {
		if opt := i.Operand.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				i.Operand = val
			}
		}
	}
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
	ctx = ctx.WithNode(e)
	if e.X == nil {
		err := errors.New("expression statement is missing expression")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	return e.X.Check(ctx.WithNode(e.X))
}

func (e *ExpressionStmt) Optimize(ctx *OptimizeContext) Node {
	if e.X != nil {
		if e.X != nil {
			if opt := e.X.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					e.X = val
				}
			}
		}
	}
	return e
}

// 注意: SwitchStmt 和 RangeStmt 已迁移至 ast_stmt.go
