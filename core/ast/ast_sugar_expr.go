package ast

import (
	"errors"
	"fmt"
	"strconv"

	"gopkg.d7z.net/go-mini/core/typespec"
)

// BinaryExpr 表示二元运算表达式 1 + 1
type BinaryExpr struct {
	BaseNode
	Operator           Ident               `json:"operator"`
	Left               Expr                `json:"left"`
	Right              Expr                `json:"right"`
	OperatorResolution *OperatorResolution `json:"-"`
}

func (b *BinaryExpr) GetBase() *BaseNode { return &b.BaseNode }
func (b *BinaryExpr) exprNode()          {}

func tryConstantFold(left, right *LiteralExpr, operator Ident, id string) *LiteralExpr {
	leftType := left.Type
	rightType := right.Type

	if (leftType == TypeInt64 && rightType == TypeInt64) ||
		(leftType == TypeFloat64 && rightType == TypeFloat64) {
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
			if leftType == TypeInt64 {
				ret.Value = strconv.FormatInt(int64(result), 10)
			}
			return ret
		}
	} else if leftType == TypeBool && rightType == TypeBool {
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
					Type: TypeBool,
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
	b.OperatorResolution = nil
	if b.Left == nil || b.Right == nil {
		err := errors.New("二元表达式缺少操作数")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	leftCtx := ctx.WithNode(b.Left)
	logCount := leftCtx.LogCount()
	if err := b.Left.Check(leftCtx); ForwardStructuredError(ctx, b.Left, logCount, err) {
		return err
	}
	rightCtx := ctx.WithNode(b.Right)
	logCount = rightCtx.LogCount()
	if err := b.Right.Check(rightCtx); ForwardStructuredError(ctx, b.Right, logCount, err) {
		return err
	}

	normalized, ok := typespec.NormalizeBinaryOperator(string(b.Operator))
	if !ok {
		ctx.AddErrorf("未知二元表达式: %s", b.Operator)
		return fmt.Errorf("未知二元表达式: %s", b.Operator)
	}
	b.Operator = Ident(normalized)

	leftType := b.Left.GetBase().Type
	rightType := b.Right.GetBase().Type

	result, nativeErr := typespec.BinaryResultType(normalized, typespec.Type(leftType), typespec.Type(rightType))
	if nativeErr == nil {
		b.Type = GoMiniType(result)
		return nil
	}

	if resolution, ok, err := ctx.ResolveBinaryOperatorMethod(normalized, leftType, rightType); ok {
		if err != nil {
			ctx.AddErrorAt(b, "%s", err.Error())
			return err
		}
		b.OperatorResolution = resolution
		b.Type = resolution.ReturnType
		return nil
	}

	if target, message, ok := binaryOperandDiagnostic(normalized, b.Left, b.Right); ok {
		ctx.AddErrorAt(target, "%s", message)
		return nativeErr
	}
	ctx.AddErrorAt(b, "%s", nativeErr.Error())
	return nativeErr
}

func binaryOperandDiagnostic(op typespec.BinaryOperator, left, right Expr) (Node, string, bool) {
	leftType := left.GetBase().Type
	rightType := right.GetBase().Type
	switch op {
	case typespec.OpAnd, typespec.OpOr:
		if leftType != TypeBool && leftType != TypeAny && leftType != "" {
			return left, fmt.Sprintf("%s operator expects Bool, got %s", op, leftType), true
		}
		if rightType != TypeBool && rightType != TypeAny && rightType != "" {
			return right, fmt.Sprintf("%s operator expects Bool, got %s", op, rightType), true
		}
	case typespec.OpMinus, typespec.OpMult, typespec.OpDiv:
		if leftType != TypeAny && leftType != "" && !leftType.IsNumeric() {
			return left, fmt.Sprintf("%s operator expects a numeric type, got %s", op, leftType), true
		}
		if rightType != TypeAny && rightType != "" && !rightType.IsNumeric() {
			return right, fmt.Sprintf("%s operator expects a numeric type, got %s", op, rightType), true
		}
	case typespec.OpMod, typespec.OpBitAnd, typespec.OpBitOr, typespec.OpBitXor, typespec.OpLsh, typespec.OpRsh:
		if leftType != TypeInt64 && leftType != TypeAny && leftType != "" {
			return left, fmt.Sprintf("%s operator expects Int64, got %s", op, leftType), true
		}
		if rightType != TypeInt64 && rightType != TypeAny && rightType != "" {
			return right, fmt.Sprintf("%s operator expects Int64, got %s", op, rightType), true
		}
	}
	return nil, "", false
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

	return b
}

// UnaryExpr 表示一元运算表达式 !true
type UnaryExpr struct {
	BaseNode
	Operator           Ident               `json:"operator"`
	Operand            Expr                `json:"operand"`
	OperatorResolution *OperatorResolution `json:"-"`
}

func (u *UnaryExpr) GetBase() *BaseNode { return &u.BaseNode }
func (u *UnaryExpr) exprNode()          {}

func (u *UnaryExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(u)
	u.OperatorResolution = nil
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
	operandCtx := ctx.WithNode(u.Operand)
	logCount := operandCtx.LogCount()
	if err := u.Operand.Check(operandCtx); ForwardStructuredError(ctx, u.Operand, logCount, err) {
		return err
	}

	operandType := u.Operand.GetBase().Type
	u.Type = operandType
	nativeOK := true
	switch u.Operator {
	case "Not":
		if operandType != TypeBool && operandType != TypeAny && operandType != "" {
			nativeOK = false
		} else {
			u.Type = TypeBool
		}
	case "Sub", "Plus":
		if operandType != TypeInt64 && operandType != TypeFloat64 && operandType != TypeAny && operandType != "" {
			nativeOK = false
		}
	case "BitXor":
		if operandType != TypeInt64 && operandType != TypeAny && operandType != "" {
			nativeOK = false
		}
	}
	if nativeOK {
		return nil
	}
	if resolution, ok, err := ctx.ResolveUnaryOperatorMethod(u.Operator, operandType); ok {
		if err != nil {
			ctx.AddErrorAt(u, "%s", err.Error())
			return err
		}
		u.OperatorResolution = resolution
		u.Type = resolution.ReturnType
		return nil
	}

	var err error
	switch u.Operator {
	case "Not":
		err = fmt.Errorf("Not operator expects Bool, got %s", operandType)
	case "Sub", "Plus":
		err = fmt.Errorf("%s operator expects a numeric type, got %s", u.Operator, operandType)
	case "BitXor":
		err = fmt.Errorf("BitXor operator expects Int64, got %s", operandType)
	default:
		err = fmt.Errorf("%s operator does not support %s", u.Operator, operandType)
	}
	ctx.AddErrorAt(u.Operand, "%s", err.Error())
	return err
}

func (u *UnaryExpr) Optimize(ctx *OptimizeContext) Node {
	if u.Operand != nil {
		if opt := u.Operand.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				u.Operand = val
			}
		}
	}

	if u.Operator == "Plus" && u.OperatorResolution == nil {
		return u.Operand
	}

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
		return errors.New("import is only allowed at package scope")
	}
	return nil
}

func (i *ImportExpr) Optimize(ctx *OptimizeContext) Node {
	return i
}
