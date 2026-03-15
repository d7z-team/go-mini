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

	// 确定 b.Type
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
			b.Type = "Bool"
			return nil
		}
	}

	if b.Operator == "And" || b.Operator == "Or" || b.Operator == "Eq" || b.Operator == "Neq" ||
		b.Operator == "Lt" || b.Operator == "Gt" || b.Operator == "Le" || b.Operator == "Ge" {
		b.Type = "Bool"
	} else {
		leftType := b.Left.GetBase().Type
		if leftType.IsPtr() {
			leftType, _ = leftType.GetPtrElementType()
		}
		if miniStruct, exists := ctx.GetStruct(Ident(leftType)); exists {
			if funct, ok := miniStruct.Methods[b.Operator]; ok {
				b.Type = funct.Returns
			}
		}
	}

	return nil
}

func (b *BinaryExpr) Optimize(ctx *OptimizeContext) Node {
	// 1. 先尝试直接对当前的 Left/Right 执行 tryConstantFold
	if leftLit, ok := b.Left.(*LiteralExpr); ok {
		if rightLit, ok := b.Right.(*LiteralExpr); ok {
			if folded := tryConstantFold(leftLit, rightLit, b.Operator, b.ID, b.Message); folded != nil {
				return folded
			}
		}
	}

	// 2. 递归优化子节点
	b.Left = b.Left.Optimize(ctx).(Expr)
	b.Right = b.Right.Optimize(ctx).(Expr)

	// 3. 再次尝试折叠 (可能子节点优化后变成了 LiteralExpr)
	if leftLit, ok := b.Left.(*LiteralExpr); ok {
		if rightLit, ok := b.Right.(*LiteralExpr); ok {
			if folded := tryConstantFold(leftLit, rightLit, b.Operator, b.ID, b.Message); folded != nil {
				return folded
			}
		}
	}

	// 4. 如果仍不是常量，将其 Lowering 为 StructCallExpr 并递归 Optimize
	isLeftNil := false
	if lit, ok := b.Left.(*LiteralExpr); ok && lit.Type.IsPtr() && lit.Value == "" {
		isLeftNil = true
	}
	isRightNil := false
	if lit, ok := b.Right.(*LiteralExpr); ok && lit.Type.IsPtr() && lit.Value == "" {
		isRightNil = true
	}
	if (b.Operator == "Eq" || b.Operator == "Neq") && (isLeftNil || isRightNil) {
		return b
	}
	if b.Operator == "And" || b.Operator == "Or" {
		return b
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
	return call.Optimize(ctx)
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
	} else if u.Operator == "Plus" || u.Operator == "Sub" || u.Operator == "BitwiseNot" {
		u.Type = u.Operand.GetBase().Type
	}

	return nil
}

func (u *UnaryExpr) Optimize(ctx *OptimizeContext) Node {
	u.Operand = u.Operand.Optimize(ctx).(Expr)

	if u.Operator == "Plus" {
		return u.Operand
	}

	call := &StructCallExpr{
		BaseNode: BaseNode{
			ID:      u.ID,
			Meta:    "struct_call",
			Message: u.Message,
			Type:    u.Type,
		},
		Name:   u.Operator,
		Object: u.Operand,
		Args:   []Expr{},
	}

	return call.Optimize(ctx)
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

	if ident, ok := s.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}
			mangledName := fmt.Sprintf("%s.%s", pkgName, s.Name)
			if funct, exists := ctx.root.ImportedFuncs[Ident(mangledName)]; exists {
				s.Type = funct.Return
				return nil
			}
			if funct, exists := ctx.GetFunction(Ident(mangledName)); exists {
				s.Type = funct.Returns
				return nil
			}
		}
	}

	miniType := s.Object.GetBase().Type
	if miniType.IsPtr() {
		miniType, _ = miniType.GetPtrElementType()
	}

	if miniType.IsAny() {
		s.Type = "Any"
		return nil
	}

	miniStruct, exists := ctx.GetStruct(Ident(miniType))
	if !exists {
		// 可能是包选择器，在 Optimize 中处理降级。
		// 这里先不报错，或者检查是否是 IdentifierExpr
		if _, ok := s.Object.(*IdentifierExpr); ok {
			return nil
		}
		ctx.AddErrorf("struct %s not found(%s).", miniType, s.ID)
		return fmt.Errorf("struct %s not found", miniType)
	}

	funct, ok := miniStruct.Methods[s.Name]
	if !ok {
		ctx.AddErrorf("struct %s function %s not found", miniType, s.Name)
		return fmt.Errorf("function %s not found", s.Name)
	}
	s.Type = funct.Returns
	return nil
}

func (s *StructCallExpr) Optimize(ctx *OptimizeContext) Node {
	// 1. Package selector check (Static Inlining)
	if ident, ok := s.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			firstChar := string(s.Name)[0]
			if firstChar < 'A' || firstChar > 'Z' {
				ctx.AddErrorf("cannot refer to unexported name %s.%s", ident.Name, s.Name)
				return nil
			}

			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}

			mangledName := fmt.Sprintf("%s.%s", pkgName, s.Name)
			fTypeStr := GoMiniType("")
			if funct, exists := ctx.root.ImportedFuncs[Ident(mangledName)]; exists {
				fTypeStr = funct.MiniType()
			} else if funct, exists := ctx.GetFunction(Ident(mangledName)); exists {
				fTypeStr = funct.MiniType()
			}

			constRef := &ConstRefExpr{
				BaseNode: BaseNode{
					ID:   s.ID + "_ref",
					Meta: "const_ref",
					Type: fTypeStr,
				},
				Name: Ident(mangledName),
			}

			callExpr := &CallExprStmt{
				BaseNode: s.BaseNode,
				Func:     constRef,
				Args:     s.Args,
			}
			callExpr.Meta = "call"
			callExpr.Type = s.Type
			return callExpr.Optimize(ctx)
		}
	}

	s.Object = s.Object.Optimize(ctx).(Expr)
	for i, arg := range s.Args {
		s.Args[i] = arg.Optimize(ctx).(Expr)
	}

	miniType := s.Object.GetBase().Type
	if miniType.IsPtr() {
		miniType, _ = miniType.GetPtrElementType()
	}

	if miniType.IsAny() {
		return s
	}

	miniStruct, _ := ctx.GetStruct(Ident(miniType))
	funct, _ := miniStruct.Methods[s.Name]

	isVariadic := funct.Variadic
	minIn := len(funct.Params)
	if isVariadic {
		minIn--
	}

	if (!isVariadic && len(funct.Params) != len(s.Args)+1) || (isVariadic && len(s.Args)+1 < minIn) {
		// 尝试隐式推导为数组参数
		if len(funct.Params) >= 2 && funct.Params[len(funct.Params)-1].IsArray() && !isVariadic {
			targetArrayType := funct.Params[len(funct.Params)-1]
			targetElem, _ := targetArrayType.ReadArrayItemType()

			for i := 0; i < len(funct.Params)-2; i++ {
				param := funct.Params[i+1]
				arg := s.Args[i]
				ptr, _ := param.AutoPtr(arg)
				s.Args[i] = ptr
			}

			variadicArgs := s.Args[len(funct.Params)-2:]
			wrappedElements := make([]CompositeElement, len(variadicArgs))
			for i, arg := range variadicArgs {
				ptr, _ := targetElem.AutoPtr(arg)
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
		}
	}

	// 尝试自动取地址 (Receiver)
	newObj, _ := funct.Params[0].AutoPtr(s.Object)
	s.Object = newObj

	// 尝试自动取地址 (Args)
	if isVariadic {
		for i := 0; i < minIn-1; i++ {
			param := funct.Params[i+1]
			arg := s.Args[i]
			arg = tryAutoNumericCast(ctx.ValidContext, param, arg)
			ptr, _ := param.AutoPtr(arg)
			s.Args[i] = ptr
		}
	} else {
		for i, param := range funct.Params[1:] {
			arg := s.Args[i]
			arg = tryAutoNumericCast(ctx.ValidContext, param, arg)
			ptr, _ := param.AutoPtr(arg)
			s.Args[i] = ptr
		}
	}

	callExpr := &CallExprStmt{
		BaseNode: BaseNode{
			ID:      s.ID,
			Meta:    "call",
			Message: s.Message,
			Type:    s.Type,
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
	return callExpr.Optimize(ctx)
}

type NewExpr struct {
	BaseNode
	Kind Ident `json:"kind"`
}

func (n *NewExpr) GetBase() *BaseNode { return &n.BaseNode }
func (n *NewExpr) exprNode()          {}

func (n *NewExpr) Check(ctx *SemanticContext) error {
	n.Kind = Ident(GoMiniType(n.Kind).Resolve(&ctx.ValidContext))
	if n.Kind == "" {
		ctx.AddErrorf("new表达式缺少类型名称")
		return fmt.Errorf("new表达式缺少类型名称")
	}

	_, ok := ctx.GetStruct(n.Kind)
	if !ok {
		ctx.AddErrorf("类型 %s 未定义", n.Kind)
		return fmt.Errorf("类型 %s 未定义", n.Kind)
	}
	n.Type = GoMiniType(n.Kind).ToPtr()
	return nil
}

func (n *NewExpr) Optimize(ctx *OptimizeContext) Node {
	a := &AddressExpr{
		BaseNode: BaseNode{
			ID:      n.ID,
			Meta:    "address",
			Message: n.Message,
			Type:    n.Type,
		},
		Operand: &CompositeExpr{
			BaseNode: BaseNode{
				ID:      n.ID + "_Operand_0",
				Meta:    "composite",
				Message: n.Message,
				Type:    GoMiniType(n.Kind),
			},
			Kind:   n.Kind,
			Values: make([]CompositeElement, 0),
		},
	}
	return a.Optimize(ctx)
}

// LiteralExpr 表示字面量表达式
type LiteralExpr struct {
	BaseNode
	Value string  `json:"value"`
	Data  MiniObj `json:"-"`
}

func (l *LiteralExpr) GetBase() *BaseNode { return &l.BaseNode }
func (l *LiteralExpr) exprNode()          {}

func (l *LiteralExpr) Check(ctx *SemanticContext) error {
	l.Type = l.Type.Resolve(&ctx.ValidContext)
	if l.Type == "" {
		ctx.AddErrorf("字面量缺少值或定义")
		return fmt.Errorf("字面量缺少值或定义")
	}
	if !l.Type.Valid(&ctx.ValidContext) {
		return fmt.Errorf("类型无效")
	}
	if l.Type.IsVoid() || l.Type.IsEmpty() {
		ctx.AddErrorf("不支持的类型 :%s", l.Type)
		return fmt.Errorf("不支持的类型 :%s", l.Type)
	}
	if _, b := l.Type.ReadFunc(); b {
		return fmt.Errorf("不支持函数类型字面量")
	}

	if l.Type.IsPtr() && l.Value == "" {
		return nil
	}

	newFuncName := Ident(fmt.Sprintf("__obj__new__%s", l.Type))
	if _, ok := ctx.root.Methods[newFuncName]; !ok {
		ctx.AddErrorf("不支持类型:%s", l.Type)
		return fmt.Errorf("不支持类型:%s", l.Type)
	}

	return nil
}

func (l *LiteralExpr) Optimize(ctx *OptimizeContext) Node {
	if l.Type.IsPtr() && l.Value == "" {
		return l
	}

	constID := ctx.ConstStore(l.Value)
	newFuncName := Ident(fmt.Sprintf("__obj__new__%s", l.Type))
	r := &CallExprStmt{
		BaseNode: BaseNode{
			Meta:    "call",
			ID:      l.ID,
			Message: l.Message,
			Type:    l.Type,
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
	return r.Optimize(ctx)
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
			// 这里由于在 Optimize 阶段，不再递归调用 Validate，直接返回。
			// 如果需要优化，可以调用 Optimize。
			return castCall
		}
	}
	return arg
}
