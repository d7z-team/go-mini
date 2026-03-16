package ast

import (
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

func tryConstantFold(left, right *LiteralExpr, operator Ident, id string, message string) *LiteralExpr {
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
					ID:      id,
					Meta:    "literal",
					Type:    leftType,
					Message: message,
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
					ID:      id,
					Meta:    "literal",
					Type:    "Bool",
					Message: message,
				},
				Value: strconv.FormatBool(result),
			}
		}
	}
	return nil
}

func (b *BinaryExpr) Check(ctx *SemanticContext) error {
	if err := b.Left.Check(ctx); err != nil {
		return err
	}
	if err := b.Right.Check(ctx); err != nil {
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
	default:
		ctx.AddErrorf("未知二元表达式: %s", b.Operator)
		return fmt.Errorf("未知二元表达式: %s", b.Operator)
	}

	if b.Operator == "And" || b.Operator == "Or" || b.Operator == "Eq" || b.Operator == "Neq" ||
		b.Operator == "Lt" || b.Operator == "Gt" || b.Operator == "Le" || b.Operator == "Ge" {
		b.Type = "Bool"
	} else {
		// 在隔离架构下，标量运算结果类型等于左操作数类型（规约后）
		b.Type = b.Left.GetBase().Type
	}

	return nil
}

func (b *BinaryExpr) Optimize(ctx *OptimizeContext) Node {
	// 1. 递归优化子节点
	b.Left = b.Left.Optimize(ctx).(Expr)
	b.Right = b.Right.Optimize(ctx).(Expr)

	// 2. 常量折叠
	if leftLit, ok := b.Left.(*LiteralExpr); ok {
		if rightLit, ok := b.Right.(*LiteralExpr); ok {
			if folded := tryConstantFold(leftLit, rightLit, b.Operator, b.ID, b.Message); folded != nil {
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
	switch u.Operator {
	case "-", "Sub", "Minus":
		u.Operator = "Sub"
	case "!", "Not":
		u.Operator = "Not"
	case "+", "Plus":
		u.Operator = "Plus"
	case "^", "BitwiseNot":
		u.Operator = "BitwiseNot"
	default:
		ctx.AddErrorf("未知一元表达式: %s", u.Operator)
		return fmt.Errorf("未知一元表达式: %s", u.Operator)
	}

	if u.Operand == nil {
		ctx.AddErrorf("一元表达式缺少操作数")
		return fmt.Errorf("一元表达式缺少操作数")
	}
	if err := u.Operand.Check(ctx); err != nil {
		return err
	}

	if u.Operator == "Not" {
		u.Type = "Bool"
	} else {
		u.Type = u.Operand.GetBase().Type
	}

	return nil
}

func (u *UnaryExpr) Optimize(ctx *OptimizeContext) Node {
	u.Operand = u.Operand.Optimize(ctx).(Expr)

	if u.Operator == "Plus" {
		return u.Operand
	}

	// 隔离架构下不再转换为 StructCallExpr
	return u
}

type StructCallExpr struct {
	BaseNode
	Object Expr   `json:"object"`
	Name   Ident  `json:"name"`
	Args   []Expr `json:"args"`
}

func (s *StructCallExpr) GetBase() *BaseNode {
	return &s.BaseNode
}
func (s *StructCallExpr) exprNode() {}

func (s *StructCallExpr) Check(ctx *SemanticContext) error {
	if !s.Name.Valid(&ctx.ValidContext) {
		return fmt.Errorf("invalid name")
	}

	if err := s.Object.Check(ctx); err != nil {
		return err
	}

	for _, arg := range s.Args {
		if err := arg.Check(ctx); err != nil {
			return err
		}
	}

	// 检查是否是包路径调用 (如 fmt.Println)
	if ident, ok := s.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}
			mangledName := fmt.Sprintf("%s.%s", pkgName, s.Name)
			if _, exists := ctx.GetFunction(Ident(mangledName)); exists {
				s.Type = "Any" // 暂时统一定义为 Any
				return nil
			}
		}
	}

	// 在隔离架构下，标量类型不再有 Methods
	miniType := s.Object.GetBase().Type
	if miniType.IsNumeric() || miniType.IsString() || miniType.IsBool() {
		return fmt.Errorf("primitive type %s has no methods", miniType)
	}

	// 仅支持真正的 Struct 调用
	miniStruct, exists := ctx.GetStruct(Ident(miniType))
	if exists {
		if funct, ok := miniStruct.Methods[s.Name]; ok {
			s.Type = funct.Returns
			return nil
		}
	}

	return fmt.Errorf("method %s not found on type %s", s.Name, miniType)
}

func (s *StructCallExpr) Optimize(ctx *OptimizeContext) Node {
	// 静态转换包调用为 CallExprStmt
	if ident, ok := s.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}
			mangledName := fmt.Sprintf("%s.%s", pkgName, s.Name)

			constRef := &ConstRefExpr{
				BaseNode: BaseNode{
					ID:   s.ID + "_ref",
					Meta: "const_ref",
					Type: "function",
				},
				Name: Ident(mangledName),
			}

			callExpr := &CallExprStmt{
				BaseNode: s.BaseNode,
				Func:     constRef,
				Args:     s.Args,
			}
			callExpr.Meta = "call"
			return callExpr.Optimize(ctx)
		}
	}

	s.Object = s.Object.Optimize(ctx).(Expr)
	for i, arg := range s.Args {
		s.Args[i] = arg.Optimize(ctx).(Expr)
	}
	return s
}

// LiteralExpr 表示字面量表达式
type LiteralExpr struct {
	BaseNode
	Value string `json:"value"`
}

func (l *LiteralExpr) GetBase() *BaseNode { return &l.BaseNode }
func (l *LiteralExpr) exprNode()          {}

func (l *LiteralExpr) Check(ctx *SemanticContext) error {
	l.Type = l.Type.Resolve(&ctx.ValidContext)
	if l.Type == "" {
		return fmt.Errorf("missing type for literal")
	}
	// 在隔离架构下，LiteralExpr 是合法的，无需构造函数检查
	return nil
}

func (l *LiteralExpr) Optimize(ctx *OptimizeContext) Node {
	// 隔离架构下直接保留字面量，不转换为 __obj__new__
	return l
}

// 移除过时的自动数值转换
func tryAutoNumericCast(ctx *ValidContext, param GoMiniType, arg Expr) Expr {
	return arg
}
