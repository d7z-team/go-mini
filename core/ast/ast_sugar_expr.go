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

func (b *BinaryExpr) Validate(ctx *ValidContext) (Node, bool) {
	leftNode, ok := b.Left.Validate(ctx)
	if !ok {
		return nil, false
	}
	b.Left = leftNode.(Expr)

	rightNode, ok := b.Right.Validate(ctx)
	if !ok {
		ctx.Child(b.Right).AddErrorf("格式错误")
		return nil, false
	}
	b.Right = rightNode.(Expr)
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
		return nil, false
	}

	leftType := b.Left.GetBase().Type
	rightType := b.Right.GetBase().Type
	// 常量折叠优化
	if leftLit, ok := b.Left.(*LiteralExpr); ok {
		if rightLit, ok := b.Right.(*LiteralExpr); ok {
			if (leftType == "Int64" && rightType == "Int64") ||
				(leftType == "Float64" && rightType == "Float64") {
				leftVal, _ := strconv.ParseFloat(leftLit.Value, 64)
				rightVal, _ := strconv.ParseFloat(rightLit.Value, 64)
				var result float64
				switch b.Operator {
				case "+", "Plus":
					result = leftVal + rightVal
				case "-", "Minus":
					result = leftVal - rightVal
				case "*", "Mult":
					result = leftVal * rightVal
				case "/", "Div":
					if rightVal == 0 {
						ctx.AddErrorf("除零错误")
						return nil, false
					}
					result = leftVal / rightVal
				}
				ret := &LiteralExpr{
					BaseNode: BaseNode{
						ID:      b.ID,
						Meta:    "literal",
						Type:    leftType,
						Message: b.Message,
					},
					Value: fmt.Sprintf("%v", result),
				}
				if leftType == "Int64" {
					ret.Value = strconv.FormatInt(int64(result), 10)
				}
				return ret, true
			} else if leftType == "Bool" && rightType == "Bool" {
				leftVal := leftLit.Value == "true"
				rightVal := rightLit.Value == "true"
				var result bool
				switch b.Operator {
				case "And", "&&":
					result = leftVal && rightVal
				case "Or", "||":
					result = leftVal || rightVal
				}
				return &LiteralExpr{
					BaseNode: BaseNode{
						ID:      b.ID,
						Meta:    "literal",
						Type:    "Bool",
						Message: b.Message,
					},
					Value: strconv.FormatBool(result),
				}, true
			}
		}
	}
	if b.Left == nil || b.Right == nil || b.Operator == "" {
		ctx.AddErrorf("二元表达式格式错误")
		return nil, false
	}

	// 特殊处理 nil 比较 (Eq/Neq)
	if b.Operator == "Eq" || b.Operator == "Neq" {
		isLeftNil := false
		if lit, ok := b.Left.(*LiteralExpr); ok && lit.Type.IsPtr() && lit.Value == "" {
			isLeftNil = true
		}
		isRightNil := false
		if lit, ok := b.Right.(*LiteralExpr); ok && lit.Type.IsPtr() && lit.Value == "" {
			isRightNil = true
		}

		if isLeftNil || isRightNil {
			// 如果一边是 nil，校验通过，且结果是 Bool
			b.Type = "Bool"
			return b, true
		}
	}

	if b.Operator == "And" || b.Operator == "Or" {
		b.Type = "Bool"
		return b, true
	}

	call := &StructCallExpr{
		BaseNode: BaseNode{
			ID:      b.ID,
			Meta:    "struct_call",
			Type:    b.Type,
			Message: b.Message,
		},
		Name:   b.Operator,
		Object: b.Left,
		Args:   []Expr{b.Right},
	}
	validate, check := call.Validate(ctx)
	if !check {
		return nil, false
	}
	return validate, true
}

// UnaryExpr 表示一元运算表达式 !true
type UnaryExpr struct {
	BaseNode
	Operator Ident `json:"operator"`
	Operand  Expr  `json:"operand"`
}

func (u *UnaryExpr) GetBase() *BaseNode { return &u.BaseNode }
func (u *UnaryExpr) exprNode()          {}

func (u *UnaryExpr) Validate(ctx *ValidContext) (Node, bool) {
	switch u.Operator {
	case "-", "Sub":
		u.Operator = "Sub"
	case "!", "Not":
		u.Operator = "Not"
	case "+", "Plus":
		u.Operator = "Plus"
	case "^", "BitwiseNot":
		u.Operator = "BitwiseNot"
	default:
		ctx.AddErrorf("未知一元表达式: %s", u.Operator)
		return nil, false
	}

	if u.Operand == nil || u.Operator == "" {
		ctx.AddErrorf("一元表达式格式错误")
		return nil, false
	}
	node, b := u.Operand.Validate(ctx)
	if !b {
		return nil, false
	}
	u.Operand = node.(Expr)

	if u.Operator == "Plus" {
		// 一元加号直接返回操作数
		return u.Operand, true
	}

	call := StructCallExpr{
		BaseNode: BaseNode{
			ID:      u.ID,
			Meta:    "struct_call",
			Message: u.Message,
		},
		Name:   u.Operator,
		Object: u.Operand,
		Args:   []Expr{},
	}

	validate, check := call.Validate(ctx)
	if !check {
		return nil, false
	}
	return validate, true
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
func (s *StructCallExpr) Validate(ctx *ValidContext) (Node, bool) {
	if !s.Name.Valid(ctx) {
		return nil, false
	}

	// 1. Package selector check (Static Inlining)
	if ident, ok := s.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			firstChar := string(s.Name)[0]
			if firstChar < 'A' || firstChar > 'Z' {
				ctx.AddErrorf("cannot refer to unexported name %s.%s", ident.Name, s.Name)
				return nil, false
			}

			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}

			mangledName := fmt.Sprintf("%s.%s", pkgName, s.Name)
			constRef := &ConstRefExpr{
				BaseNode: BaseNode{
					ID:   s.ID + "_ref",
					Meta: "const_ref",
				},
				Name: Ident(mangledName),
			}

			callExpr := &CallExprStmt{
				BaseNode: s.BaseNode,
				Func:     constRef,
				Args:     s.Args,
			}
			callExpr.Meta = "call"
			return callExpr.Validate(ctx)
		}
	}

	obj, b := s.Object.Validate(ctx)
	if !b {
		return nil, false
	}
	for i, arg := range s.Args {
		validate, b := arg.Validate(ctx)
		if !b {
			return nil, false
		}
		s.Args[i] = validate.(Expr)
	}

	s.Object = obj.(Expr)
	miniType := s.Object.GetBase().Type
	if miniType.IsPtr() {
		miniType, _ = miniType.GetPtrElementType()
	}

	if miniType.IsAny() {
		// Any 类型在运行时解析方法，校验通过
		s.Type = "Any"
		return s, true
	}

	miniStruct, exists := ctx.GetStruct(Ident(miniType))
	if !exists {
		ctx.AddErrorf("struct %s not found(%s).", miniType, s.ID)
		return nil, false
	}
	funct, ok := miniStruct.Methods[s.Name]
	if !ok {
		ctx.AddErrorf("struct %s function %s not found", miniType, s.Name)
		return nil, false
	}

	isVariadic := funct.Variadic
	minIn := len(funct.Params)
	if isVariadic {
		minIn-- // 变长参数可以传入 0 个或多个
	}

	if (!isVariadic && len(funct.Params) != len(s.Args)+1) || (isVariadic && len(s.Args)+1 < minIn) {
		// 尝试隐式推导为数组参数
		if len(funct.Params) >= 2 && funct.Params[len(funct.Params)-1].IsArray() && !isVariadic {
			targetArrayType := funct.Params[len(funct.Params)-1]
			targetElem, _ := targetArrayType.ReadArrayItemType()

			// 尝试自动推导前面的固定参数
			for i := 0; i < len(funct.Params)-2; i++ {
				param := funct.Params[i+1]
				arg := s.Args[i]
				ptr, b2 := param.AutoPtr(arg)
				if !b2 {
					argsType := []GoMiniType{obj.GetBase().Type}
					for _, param := range s.Args {
						argsType = append(argsType, param.GetBase().Type)
					}
					ctx.Child(s).AddErrorf("函数参数不一致:call(%v) != %s(%v)", argsType, s.Name, funct.Params)
					return nil, false
				}
				s.Args[i] = ptr
			}

			// 打包剩余参数到数组
			variadicArgs := s.Args[len(funct.Params)-2:]
			wrappedElements := make([]CompositeElement, len(variadicArgs))
			for i, arg := range variadicArgs {
				ptr, b2 := targetElem.AutoPtr(arg)
				if !b2 {
					argsType := []GoMiniType{obj.GetBase().Type}
					for _, param := range s.Args {
						argsType = append(argsType, param.GetBase().Type)
					}
					ctx.Child(s).AddErrorf("函数参数不一致:call(%v) != %s(%v)", argsType, s.Name, funct.Params)
					return nil, false
				}
				wrappedElements[i] = CompositeElement{Value: ptr}
			}

			s.Args = append(s.Args[:len(funct.Params)-2], &CompositeExpr{
				BaseNode: BaseNode{
					ID:   s.ID + "_ArgsWrap",
					Meta: "composite",
					Type: targetArrayType,
				},
				Kind:   Ident(targetArrayType),
				Values: wrappedElements,
			})
		} else {
			argsType := []GoMiniType{obj.GetBase().Type}
			for _, param := range s.Args {
				argsType = append(argsType, param.GetBase().Type)
			}
			ctx.Child(s).AddErrorf("函数参数不一致:call(%v) != %s(%v)", argsType, s.Name, funct.Params)
			return nil, false
		}
	}

	// 尝试自动取地址 (Receiver)
	newObj, ok := funct.Params[0].AutoPtr(obj.(Expr))
	if !ok {
		ctx.Child(s).AddErrorf("函数结构错误(receiver %v) != (%v)", funct.Params[0], obj.GetBase().Type)
		return nil, false
	}
	s.Object = newObj

	// 尝试自动取地址 (Args)
	if isVariadic {
		// 固定参数部分
		for i := 0; i < minIn-1; i++ {
			param := funct.Params[i+1]
			arg := s.Args[i]

			// 尝试自动数值转换
			arg = tryAutoNumericCast(ctx, param, arg)

			ptr, b2 := param.AutoPtr(arg)
			if !b2 {
				ctx.Child(s).AddErrorf("函数结构错误(arg %d: %v) != (%v)", i, param, arg.GetBase().Type)
				return nil, false
			}
			s.Args[i] = ptr
		}
		// 变长参数部分由后续的 callExpr.Validate 处理
	} else {
		for i, param := range funct.Params[1:] {
			arg := s.Args[i]

			// 尝试自动数值转换
			arg = tryAutoNumericCast(ctx, param, arg)

			ptr, b2 := param.AutoPtr(arg)
			if !b2 {
				ctx.Child(s).AddErrorf("函数结构错误(arg %d: %v) != (%v)", i, param, arg.GetBase().Type)
				return nil, false
			}
			s.Args[i] = ptr
		}
	}

	callExpr := &CallExprStmt{
		BaseNode: BaseNode{
			ID:      s.ID,
			Meta:    "call",
			Message: s.Message,
		},
		Func: &ConstRefExpr{
			BaseNode: BaseNode{
				ID:      s.ID + "_Func_0",
				Meta:    "const_ref",
				Message: s.Message,
			},
			Name: Ident(fmt.Sprintf("__obj__%s__%s", miniType, s.Name)),
		},
		Args: append([]Expr{s.Object}, s.Args...),
	}
	validate, b2 := callExpr.Validate(ctx)
	if !b2 {
		return nil, false
	}
	return validate, true
}

type NewExpr struct {
	BaseNode
	Kind Ident `json:"kind"`
}

func (n *NewExpr) GetBase() *BaseNode { return &n.BaseNode }
func (n *NewExpr) exprNode()          {}

func (n *NewExpr) Validate(ctx *ValidContext) (Node, bool) {
	n.Kind = Ident(GoMiniType(n.Kind).Resolve(ctx))
	if n.Kind == "" {
		ctx.AddErrorf("new表达式缺少类型名称")
		return nil, false
	}

	// 验证类型是否存在
	_, ok := ctx.GetStruct(n.Kind)
	if !ok {
		ctx.AddErrorf("类型 %s 未定义", n.Kind)
		return nil, false
	}
	a := &AddressExpr{
		BaseNode: BaseNode{
			ID:      n.ID,
			Meta:    "address",
			Message: n.Message,
		},
		Operand: &CompositeExpr{
			BaseNode: BaseNode{
				ID:      n.ID + "_Operand_0",
				Meta:    "composite",
				Message: n.Message,
			},
			Kind:   n.Kind,
			Values: make([]CompositeElement, 0),
		},
	}
	validate, b := a.Validate(ctx)
	if !b {
		return nil, false
	}
	return validate, true
}

// LiteralExpr 表示字面量表达式
type LiteralExpr struct {
	BaseNode
	Value string  `json:"value"`
	Data  MiniObj `json:"-"`
}

func (l *LiteralExpr) GetBase() *BaseNode { return &l.BaseNode }
func (l *LiteralExpr) exprNode()          {}

func (l *LiteralExpr) Validate(ctx *ValidContext) (Node, bool) {
	l.Type = l.Type.Resolve(ctx)
	if l.Type == "" {
		ctx.AddErrorf("字面量缺少值或定义")
		return nil, false
	}
	if !l.Type.Valid(ctx) {
		return nil, false
	}
	if l.Type.IsVoid() || l.Type.IsEmpty() {
		ctx.AddErrorf("不支持的类型 :%s", l.Type)
		return nil, false
	}
	if _, b := l.Type.ReadFunc(); b {
		return nil, false
	}

	if l.Type.IsPtr() && l.Value == "" {
		return l, true
	}

	newFuncName := Ident(fmt.Sprintf("__obj__new__%s", l.Type))
	if _, ok := ctx.root.Methods[newFuncName]; !ok {
		ctx.AddErrorf("不支持类型:%s", l.Type)
		return nil, false
	}

	constID := ctx.ConstStore(l.Value)
	r := &CallExprStmt{
		BaseNode: BaseNode{
			Meta:    "call",
			ID:      l.ID,
			Message: l.Message,
		},
		Func: &ConstRefExpr{
			BaseNode: BaseNode{
				ID:      l.ID + "_Func_0",
				Meta:    "const_ref",
				Message: l.Message,
			},
			Name: newFuncName,
		},
		Args: []Expr{&ConstRefExpr{
			BaseNode: BaseNode{
				Meta:    "const_ref",
				ID:      l.ID + "_Args_0",
				Message: l.Message,
			},
			Name: constID,
		}},
	}
	validate, b := r.Validate(ctx)
	if !b {
		return nil, false
	}
	return validate, true
}

func tryAutoNumericCast(ctx *ValidContext, param GoMiniType, arg Expr) Expr {
	argType := arg.GetBase().Type
	targetBase := param
	if param.IsPtr() {
		targetBase, _ = param.GetPtrElementType()
	}

	if targetBase.IsNumeric() && argType.IsNumeric() && !targetBase.Equals(argType) {
		newFuncName := Ident(fmt.Sprintf("__obj__new__%s", targetBase))
		if _, b := ctx.GetFunction(newFuncName); b {
			castCall := &CallExprStmt{
				BaseNode: BaseNode{
					ID:      arg.GetBase().ID + "_AutoCast",
					Meta:    "call",
					Type:    targetBase,
					Message: "Auto Cast for " + string(targetBase),
				},
				Func: &ConstRefExpr{
					BaseNode: BaseNode{
						ID:   arg.GetBase().ID + "_AutoCast_Func",
						Meta: "const_ref",
						Type: GoMiniType(fmt.Sprintf("function(Any) %s", targetBase)),
					},
					Name: newFuncName,
				},
				Args: []Expr{arg},
			}
			v, ok := castCall.Validate(ctx)
			if ok {
				return v.(Expr)
			}
		}
	}
	return arg
}
