package ast

import (
	"errors"
	"fmt"
	"strconv"
)

// BinaryExpr 表示二元运算表达式 1 + 1
type BinaryExpr struct {
	BaseNode
	Operator Ident `json:"operator"`
	Left     Expr  `json:"left"`
	Right    Expr  `json:"right"`
}

func (b *BinaryExpr) GetBase() *BaseNode { return &b.BaseNode }
func (b *BinaryExpr) exprNode()          {}

func tryConstantFold(left, right *LiteralExpr, operator Ident, id string) *LiteralExpr {
	leftType := left.Type
	rightType := right.Type

	if (leftType == "Int64" && rightType == "Int64") ||
		(leftType == "Float64" && rightType == "Float64") {
		leftVal, errL := strconv.ParseFloat(left.Value, 64)
		rightVal, errR := strconv.ParseFloat(right.Value, 64)
		if errL != nil || errR != nil {
			return nil
		}
		var result float64
		hasResult := true
		switch operator {
		case "Plus":
			result = leftVal + rightVal
		case "Minus":
			result = leftVal - rightVal
		case "Mult":
			result = leftVal * rightVal
		case "Div":
			if rightVal != 0 {
				result = leftVal / rightVal
			} else {
				hasResult = false
			}
		case "Mod":
			if rightVal != 0 {
				result = float64(int64(leftVal) % int64(rightVal))
			} else {
				hasResult = false
			}
		default:
			hasResult = false
		}
		if hasResult {
			ret := &LiteralExpr{
				BaseNode: BaseNode{
					ID:   id,
					Meta: "literal",
					Type: leftType,
					Loc:  left.Loc,
				},
				Value: fmt.Sprintf("%v", result),
			}
			if leftType == "Int64" {
				ret.Value = strconv.FormatInt(int64(result), 10)
			}
			return ret
		}
	} else if leftType == "Bool" && rightType == "Bool" {
		leftVal := left.Value == "true"
		rightVal := right.Value == "true"
		var result bool
		hasResult := true
		switch operator {
		case "And":
			result = leftVal && rightVal
		case "Or":
			result = leftVal || rightVal
		default:
			hasResult = false
		}
		if hasResult {
			return &LiteralExpr{
				BaseNode: BaseNode{
					ID:   id,
					Meta: "literal",
					Type: "Bool",
					Loc:  left.Loc,
				},
				Value: strconv.FormatBool(result),
			}
		}
	}
	return nil
}

func (b *BinaryExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(b)
	if err := b.Left.Check(ctx.WithNode(b.Left)); err != nil {
		return err
	}
	if err := b.Right.Check(ctx.WithNode(b.Right)); err != nil {
		return err
	}

	// 标准化操作符
	switch b.Operator {
	case "+", "Plus":
		b.Operator = "Plus"
	case "-", "Minus":
		b.Operator = "Minus"
	case "*", "Mult":
		b.Operator = "Mult"
	case "/", "Div":
		b.Operator = "Div"
	case "%", "Mod":
		b.Operator = "Mod"
	case "==", "Eq":
		b.Operator = "Eq"
	case "!=", "Neq":
		b.Operator = "Neq"
	case "<", "Lt":
		b.Operator = "Lt"
	case ">", "Gt":
		b.Operator = "Gt"
	case "<=", "Le":
		b.Operator = "Le"
	case ">=", "Ge":
		b.Operator = "Ge"
	case "&&", "And":
		b.Operator = "And"
	case "||", "Or":
		b.Operator = "Or"
	case "&", "BitAnd":
		b.Operator = "BitAnd"
	case "|", "BitOr":
		b.Operator = "BitOr"
	case "^", "BitXor":
		b.Operator = "BitXor"
	case "<<", "Lsh":
		b.Operator = "Lsh"
	case ">>", "Rsh":
		b.Operator = "Rsh"
	default:
		ctx.AddErrorf("未知二元表达式: %s", b.Operator)
		return fmt.Errorf("未知二元表达式: %s", b.Operator)
	}

	if b.Operator == "And" || b.Operator == "Or" || b.Operator == "Eq" || b.Operator == "Neq" ||
		b.Operator == "Lt" || b.Operator == "Gt" || b.Operator == "Le" || b.Operator == "Ge" ||
		b.Operator == "&&" || b.Operator == "||" || b.Operator == "==" || b.Operator == "!=" ||
		b.Operator == "<" || b.Operator == ">" || b.Operator == "<=" || b.Operator == ">=" {
		b.Type = "Bool"
	} else {
		// 在隔离架构下，标量运算结果类型等于左操作数类型（规约后）
		b.Type = b.Left.GetBase().Type
	}

	return nil
}

func (b *BinaryExpr) Optimize(ctx *OptimizeContext) Node {
	// 1. 递归优化子节点
	if b.Left != nil {
		if opt := b.Left.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				b.Left = val
			}
		}
	}
	if b.Right != nil {
		if opt := b.Right.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				b.Right = val
			}
		}
	}

	// 2. 常量折叠
	if leftLit, ok := b.Left.(*LiteralExpr); ok {
		if rightLit, ok := b.Right.(*LiteralExpr); ok {
			if folded := tryConstantFold(leftLit, rightLit, b.Operator, b.ID); folded != nil {
				return folded
			}
		}
	}

	// 3. 在隔离架构下，我们不再将标量运算转换为 StructCallExpr
	// 直接返回优化后的 BinaryExpr，由 Runtime 处理。
	return b
}

// UnaryExpr 表示一元运算表达式 !true
type UnaryExpr struct {
	BaseNode
	Operator Ident `json:"operator"`
	Operand  Expr  `json:"operand"`
}

func (u *UnaryExpr) GetBase() *BaseNode { return &u.BaseNode }
func (u *UnaryExpr) exprNode()          {}

func (u *UnaryExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(u)
	switch u.Operator {
	case "-", "Sub", "Minus":
		u.Operator = "Sub"
	case "!", "Not":
		u.Operator = "Not"
	case "+", "Plus":
		u.Operator = "Plus"
	case "^", "BitXor", "BitwiseNot":
		u.Operator = "BitXor"
	default:
		ctx.AddErrorf("未知一元表达式: %s", u.Operator)
		return fmt.Errorf("未知一元表达式: %s", u.Operator)
	}

	if u.Operand == nil {
		ctx.AddErrorf("一元表达式缺少操作数")
		return errors.New("一元表达式缺少操作数")
	}
	if err := u.Operand.Check(ctx.WithNode(u.Operand)); err != nil {
		return err
	}

	u.Type = u.Operand.GetBase().Type
	switch u.Operator {
	case "Not":
		if u.Type != "Bool" && u.Type != "Any" && u.Type != "" {
			ctx.AddErrorf("Not 运算符预期 Bool, 实际为 %s", u.Type)
		}
		u.Type = "Bool"
	case "Sub", "Plus":
		if u.Type != "Int64" && u.Type != "Float64" && u.Type != "Any" && u.Type != "" {
			ctx.AddErrorf("%s 运算符预期数值类型, 实际为 %s", u.Operator, u.Type)
		}
	}

	return nil
}

func (u *UnaryExpr) Optimize(ctx *OptimizeContext) Node {
	if u.Operand != nil {
		if opt := u.Operand.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				u.Operand = val
			}
		}
	}

	if u.Operator == "Plus" {
		return u.Operand
	}

	// 隔离架构下不再转换为 StructCallExpr
	return u
}

// LiteralExpr 表示字面量表达式
type LiteralExpr struct {
	BaseNode
	Value string `json:"value"`
}

func (l *LiteralExpr) GetBase() *BaseNode { return &l.BaseNode }
func (l *LiteralExpr) exprNode()          {}

func (l *LiteralExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(l)
	l.Type = l.Type.Resolve(ctx.ValidContext)
	if l.Type == "" {
		return errors.New("missing type for literal")
	}
	// 在隔离架构下，LiteralExpr 是合法的，无需构造函数检查
	return nil
}

func (l *LiteralExpr) Optimize(ctx *OptimizeContext) Node {
	// 隔离架构下直接保留字面量，不转换为 __obj__new__
	return l
}

// ImportExpr 表示导入模块表达式
type ImportExpr struct {
	BaseNode
	Path string `json:"path"`
}

func (i *ImportExpr) GetBase() *BaseNode { return &i.BaseNode }
func (i *ImportExpr) exprNode()          {}

func (i *ImportExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(i)
	i.Type = TypeModule
	if ctx.parent != nil {
		return errors.New("import 只能在包级作用域中使用")
	}
	return nil
}

func (i *ImportExpr) Optimize(ctx *OptimizeContext) Node {
	return i
}
