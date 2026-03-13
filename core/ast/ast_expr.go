package ast

import (
	"fmt"
	"strings"
)

type IdentifierExpr struct {
	BaseNode
	Name Ident `json:"name"`
}

func (c *IdentifierExpr) GetBase() *BaseNode { return &c.BaseNode }
func (c *IdentifierExpr) exprNode()          {}

func (c *IdentifierExpr) Validate(ctx *ValidContext) (Node, bool) {
	c.Name = c.Name.Resolve(ctx)
	if !c.Name.Valid(ctx) {
		return nil, false
	}
	vtp, b := ctx.GetVariable(c.Name)
	if !b {
		// 回退：尝试在当前包空间查找
		if !strings.Contains(string(c.Name), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
			mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
			if vt, ok := ctx.GetVariable(mangled); ok {
				c.Name = mangled // 关键：更新为转义后的名称
				c.Type = vt
				return c, true
			}
		}

		ctx.Child(c).AddErrorf("变量 %s 不存在", c.Name)
		return nil, false
	}

	// 特殊处理：如果是顶级变量且未带包名（同一包内访问），也补齐包名
	if !strings.Contains(string(c.Name), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
		if _, ok := ctx.root.vars[mangled]; ok {
			c.Name = mangled
		}
	}

	c.Type = vtp
	return c, true
}

type ConstRefExpr struct {
	BaseNode
	Name Ident `json:"name"`
}

func (c *ConstRefExpr) GetBase() *BaseNode { return &c.BaseNode }
func (c *ConstRefExpr) exprNode()          {}

func (c *ConstRefExpr) Validate(ctx *ValidContext) (Node, bool) {
	c.Name = c.Name.Resolve(ctx)
	if !c.Name.Valid(ctx) {
		return nil, false
	}
	if vtp, b := ctx.GetVariable(c.Name); b {
		c.Type = vtp
		return c, true
	}

	if vtp, b := ctx.GetFunction(c.Name); b {
		c.Type = vtp.MiniType()
		return c, true
	}

	if ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
		if vtp, b := ctx.GetFunction(mangled); b {
			c.Name = mangled
			c.Type = vtp.MiniType()
			return c, true
		}
	}

	if _, b := ctx.root.program.Constants[string(c.Name)]; b {
		c.Type = "Constant"
		return c, true
	}

	if ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
		if _, b := ctx.root.program.Constants[string(mangled)]; b {
			c.Name = mangled
			c.Type = "Constant"
			return c, true
		}
	}

	// 支持类型转换/构造函数语法: T(x) -> __obj__new__T(x)
	structName := c.Name
	if _, b := ctx.GetStruct(structName); !b {
		// 尝试首字母大写转换 (如 int64 -> Int64)
		s := string(c.Name)
		if len(s) > 0 {
			upperName := Ident(strings.ToUpper(s[:1]) + s[1:])
			if _, b2 := ctx.GetStruct(upperName); b2 {
				structName = upperName
			}
		}
	}

	if _, b := ctx.GetStruct(structName); b {
		newName := Ident(fmt.Sprintf("__obj__new__%s", structName))
		if vtp, b2 := ctx.GetFunction(newName); b2 {
			c.Name = newName
			c.Type = vtp.MiniType()
			return c, true
		}
	}

	ctx.Child(c).AddErrorf("const/function %s 不存在", c.Name)
	return nil, false
}

// CallExprStmt 表示函数调用表达式
type CallExprStmt struct {
	BaseNode
	Func Expr   `json:"func"` // 被调用表达式
	Args []Expr `json:"args"` // 调用参数
}

func (c *CallExprStmt) GetBase() *BaseNode { return &c.BaseNode }
func (c *CallExprStmt) exprNode()          {}
func (c *CallExprStmt) stmtNode()          {}

func (c *CallExprStmt) Validate(ctx *ValidContext) (Node, bool) {
	if c.Func == nil {
		ctx.AddErrorf("函数调用缺少函数名")
		return nil, false
	}

	funcNode, ok := c.Func.Validate(ctx)
	if !ok {
		return nil, false
	}
	c.Func = funcNode.(Expr)

	fType, b := c.Func.GetBase().Type.ReadCallFunc()
	if !b {
		ctx.Child(c.Func).AddErrorf("对象(%s)不是函数", c.Func.GetBase().Type)
		return nil, false
	}

	for i, arg := range c.Args {
		argNode, ok := arg.Validate(ctx)
		if !ok {
			return nil, false
		}
		c.Args[i] = argNode.(Expr)
	}

	var args []GoMiniType
	for _, arg := range c.Args {
		args = append(args, arg.GetBase().Type)
	}

	// 变长参数支持 (Variadic Support)
	if fType.Variadic {
		paramCount := len(fType.Params)
		// 如果参数数量正好相等，且最后一个参数已经是目标数组类型，则不需要再次打包
		alreadyPacked := false
		if len(c.Args) == paramCount {
			lastArgType := c.Args[paramCount-1].GetBase().Type
			if lastArgType.IsArray() {
				// 简单的类型检查，如果是 Array 则认为可能已经打包
				alreadyPacked = true
			}
		}

		if !alreadyPacked && len(c.Args) >= paramCount-1 {
			fixedParams := fType.Params[:paramCount-1]
			variadicParam := fType.Params[paramCount-1] // Expected to be Array<...>

			// 1. 校验固定参数
			for i := 0; i < len(fixedParams); i++ {
				ptr, ok := fixedParams[i].AutoPtr(c.Args[i])
				if !ok {
					ctx.Child(c.Func).AddErrorf("函数参数不一致 (%s) != (%s)", fType.Params, args)
					return nil, false
				}
				c.Args[i] = ptr
			}

			// 2. 打包剩余参数到数组
			variadicArgs := c.Args[paramCount-1:]
			targetElem, _ := variadicParam.ReadArrayItemType()

			wrappedElements := make([]CompositeElement, len(variadicArgs))
			for i, arg := range variadicArgs {
				ptr, ok := targetElem.AutoPtr(arg)
				if !ok {
					// 即使 AutoPtr 失败，我们也尝试保留原样，由后续逻辑报错
					wrappedElements[i] = CompositeElement{Value: arg}
				} else {
					wrappedElements[i] = CompositeElement{Value: ptr}
				}
			}

			variadicWrapper := &CompositeExpr{
				BaseNode: BaseNode{
					ID:   c.ID + "_VariadicWrap",
					Meta: "composite",
					Type: variadicParam,
				},
				Kind:   Ident(variadicParam),
				Values: wrappedElements,
			}

			c.Args = append(c.Args[:paramCount-1], variadicWrapper)
			c.Type = fType.Returns
			return c, true
		}
	}

	// 尝试隐式推导为数组参数（引擎不要可变参数概念，统一推导为 Array，callNative 单独处理）
	if len(fType.Params) == 1 && fType.Params[0].IsArray() {
		targetArrayType := fType.Params[0]
		targetElem, _ := targetArrayType.ReadArrayItemType()

		isPerfectMatch := false
		if len(c.Args) == 1 {
			_, b2 := targetArrayType.AutoPtr(c.Args[0])
			if b2 {
				isPerfectMatch = true
			}
		}

		if !isPerfectMatch {
			var deducedElem GoMiniType = "Any"
			if len(c.Args) > 0 {
				deducedElem = args[0]
				for i := 1; i < len(args); i++ {
					if args[i] != deducedElem {
						deducedElem = "Any"
						break
					}
				}
			}
			deducedArray := CreateArrayType(deducedElem)

			if !targetArrayType.Equals(deducedArray) {
				ctx.Child(c.Func).AddErrorf("函数参数不一致 (%s) != (%s)", fType.Params, []GoMiniType{deducedArray})
				return nil, false
			}

			newArgs := make([]Expr, len(c.Args))
			for i, arg := range c.Args {
				ptr, b2 := targetElem.AutoPtr(arg)
				if !b2 {
					ctx.Child(c.Func).AddErrorf("函数参数不一致 (%s) != (%s)", fType.Params, args)
					return nil, false
				}
				newArgs[i] = ptr
			}

			// Wrap into a single array argument
			wrappedArgs := make([]CompositeElement, len(newArgs))
			for i, arg := range newArgs {
				wrappedArgs[i] = CompositeElement{Value: arg}
			}
			c.Args = []Expr{&CompositeExpr{
				BaseNode: BaseNode{
					ID:   c.ID + "_ArgsWrap",
					Meta: "composite",
					Type: targetArrayType,
				},
				Kind:   Ident(targetArrayType),
				Values: wrappedArgs,
			}}
			c.Type = fType.Returns
			return c, true
		}
	}

	if len(c.Args) != len(fType.Params) {
		ctx.Child(c.Func).AddErrorf("函数参数不一致 (%s) != (%s)", fType.Params, args)
		return nil, false
	}

	for i, param := range fType.Params {
		arg := c.Args[i]
		argType := arg.GetBase().Type

		// 尝试自动数值转换 (Numeric Inter-op)
		// 如果参数需要数值类型的指针或值，但提供的是另一种数值类型
		targetBase := param
		if param.IsPtr() {
			targetBase, _ = param.GetPtrElementType()
		}

		if targetBase.IsNumeric() && argType.IsNumeric() && !targetBase.Equals(argType) {
			// 插入一个显式的构造函数调用来转换类型: targetBase(arg)
			newFuncName := Ident(fmt.Sprintf("__obj__new__%s", targetBase))
			if _, b := ctx.GetFunction(newFuncName); b {
				arg = &CallExprStmt{
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
				// 转换后重新 Validate
				v, _ := arg.Validate(ctx)
				arg = v.(Expr)
			}
		}

		ptr, b2 := param.AutoPtr(arg)
		if !b2 {
			ctx.Child(c.Func).AddErrorf("函数结构错误(%v) != (%v)", param, arg.GetBase().Type)
			return nil, false
		}
		c.Args[i] = ptr
	}

	c.Type = fType.Returns

	// 自动解包 (T, Error) 为 T
	if c.Type.IsTuple() {
		types, ok := c.Type.ReadTuple()
		if ok && len(types) == 2 && types[1] == "Error" {
			c.Type = types[0]
		}
	}

	return c, true
}

// MemberExpr 表示成员访问表达式 (a.b)
type MemberExpr struct {
	BaseNode
	Object   Expr  `json:"object"`
	Property Ident `json:"property"`
}

func (m *MemberExpr) GetBase() *BaseNode { return &m.BaseNode }
func (m *MemberExpr) exprNode()          {}

func (m *MemberExpr) Validate(ctx *ValidContext) (Node, bool) {
	if m.Object == nil {
		ctx.AddErrorf("成员访问缺少对象表达式")
		return nil, false
	}
	if m.Property == "" {
		ctx.AddErrorf("成员访问缺少属性名")
		return nil, false
	}

	// 1. Package selector check (Static Inlining)
	if ident, ok := m.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			// Check visibility (exported name must start with upper case)
			firstChar := string(m.Property)[0]
			if firstChar < 'A' || firstChar > 'Z' {
				ctx.AddErrorf("cannot refer to unexported name %s.%s", ident.Name, m.Property)
				return nil, false
			}

			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}

			mangledName := fmt.Sprintf("%s.%s", pkgName, m.Property)
			constRef := &ConstRefExpr{
				BaseNode: BaseNode{
					ID:   m.ID,
					Meta: "const_ref",
				},
				Name: Ident(mangledName),
			}
			return constRef.Validate(ctx)
		}
	}

	objNode, ok := m.Object.Validate(ctx)
	if !ok {
		return nil, false
	}
	m.Object = objNode.(Expr)

	if !m.Property.Valid(ctx) {
		return nil, false
	}

	name, b := m.Object.GetBase().Type.StructName()
	if !b {
		ctx.Child(m.Object).AddErrorf("不是合法的 struct (%s)", m.Object.GetBase().Type)
		return nil, false
	}

	miniStruct, b := ctx.GetStruct(name)
	if !b {
		ctx.AddErrorf("未定义 struct (%s)", name)
		return nil, false
	}
	// 成员不存在
	met, b := miniStruct.Fields[m.Property]
	if !b {
		if method, ok := miniStruct.Methods[m.Property]; ok {
			met = method.MiniType()
		} else {
			ctx.AddErrorf("struct(%s) 不存在 (%s)", name, m.Property)
			return nil, false
		}
	}

	m.Type = met
	return m, true
}

// CompositeExpr 表示复合类型表达式
// 复合类型支持普通对象 / Array对象 / Map
type CompositeExpr struct {
	BaseNode
	Kind   Ident              `json:"type"`
	Values []CompositeElement `json:"values,omitempty"`
}

// CompositeElement 表示复合类型的元素
type CompositeElement struct {
	Key   Expr `json:"key,omitempty"`
	Value Expr `json:"value"`
}

func (c *CompositeExpr) GetBase() *BaseNode { return &c.BaseNode }
func (c *CompositeExpr) exprNode()          {}

func (c *CompositeExpr) Validate(ctx *ValidContext) (Node, bool) {
	c.Kind = Ident(GoMiniType(c.Kind).Resolve(ctx))
	c.Type = GoMiniType(c.Kind) // 同步更新 BaseNode.Type
	if c.Kind == "" {
		ctx.AddErrorf("复合类型缺少类型标识")
		return nil, false
	}
	for i, elem := range c.Values {
		if elem.Key != nil {
			keyNode, ok := elem.Key.Validate(ctx)
			if !ok {
				return nil, false
			}
			c.Values[i].Key = keyNode.(Expr)
		}
		if elem.Value == nil {
			ctx.AddErrorf("复合类型元素缺少值")
			return nil, false
		}
		valNode, ok := elem.Value.Validate(ctx)
		if !ok {
			return nil, false
		}
		c.Values[i].Value = valNode.(Expr)
	}

	return c, true
}

// IndexExpr 表示索引访问表达式 i[1]
type IndexExpr struct {
	BaseNode
	Object Expr `json:"object"`
	Index  Expr `json:"index"`
}

func (i *IndexExpr) GetBase() *BaseNode { return &i.BaseNode }
func (i *IndexExpr) exprNode()          {}

func (i *IndexExpr) Validate(ctx *ValidContext) (Node, bool) {
	if i.Object == nil {
		ctx.AddErrorf("索引访问缺少对象表达式")
		return nil, false
	}

	objNode, ok := i.Object.Validate(ctx)
	if !ok {
		return nil, false
	}
	i.Object = objNode.(Expr)

	if i.Index == nil {
		ctx.AddErrorf("索引访问缺少索引表达式")
		return nil, false
	}

	idxNode, ok := i.Index.Validate(ctx)
	if !ok {
		return nil, false
	}
	i.Index = idxNode.(Expr)

	// 检查对象是否为数组类型
	objType := i.Object.GetBase().Type
	if objType.IsPtr() {
		if elem, ok := objType.GetPtrElementType(); ok {
			objType = elem
		}
	}

	if objType.IsArray() {
		// 检查索引类型是否为 Int64
		if i.Index.GetBase().Type != "Int64" {
			ctx.Child(i.Index).AddErrorf("数组索引只支持 Int64 类型 (%s)", i.Index.GetBase().Type)
			return nil, false
		}

		if elemType, ok := objType.ReadArrayItemType(); ok {
			i.Type = elemType
		} else {
			ctx.Child(i.Object).AddErrorf("无法获取数组元素类型: %s", objType)
			return nil, false
		}
		return i, true
	}
	if objType.IsMap() {
		keyType, valType, ok := objType.GetMapKeyValueTypes()
		if !ok {
			ctx.Child(i.Object).AddErrorf("无法获取Map类型信息: %s", objType)
			return nil, false
		}
		if !i.Index.GetBase().Type.Equals(keyType) {
			ctx.Child(i.Index).AddErrorf("Map索引类型不匹配: 需 %s, 实际 %s", keyType, i.Index.GetBase().Type)
			return nil, false
		}
		i.Type = valType
		return i, true
	}
	ctx.Child(i.Object).AddErrorf("索引访问的对象必须是数组或Map类型，实际为 %s", objType)
	return nil, false
}

// AddressExpr 表示取地址表达式 &x
type AddressExpr struct {
	BaseNode
	Operand Expr `json:"operand"`
}

func (a *AddressExpr) GetBase() *BaseNode { return &a.BaseNode }
func (a *AddressExpr) exprNode()          {}

func (a *AddressExpr) Validate(ctx *ValidContext) (Node, bool) {
	if a.Operand == nil {
		ctx.AddErrorf("取地址表达式缺少操作数")
		return nil, false
	}
	operandNode, ok := a.Operand.Validate(ctx)
	if !ok {
		return nil, false
	}
	a.Operand = operandNode.(Expr)
	if a.Operand.GetBase().Type.IsPtr() {
		ctx.AddErrorf("取地址操作符 & 只能用于左值")
		return nil, false
	}
	// 设置类型为指向操作数类型的指针
	a.Type = a.Operand.GetBase().Type.ToPtr()

	return a, true
}

// DerefExpr 表示解引用表达式 *p
type DerefExpr struct {
	BaseNode
	Operand Expr `json:"operand"`
}

func (d *DerefExpr) GetBase() *BaseNode { return &d.BaseNode }
func (d *DerefExpr) exprNode()          {}

func (d *DerefExpr) Validate(ctx *ValidContext) (Node, bool) {
	if d.Operand == nil {
		ctx.AddErrorf("解引用表达式缺少操作数")
		return nil, false
	}

	operandNode, ok := d.Operand.Validate(ctx)
	if !ok {
		return nil, false
	}
	d.Operand = operandNode.(Expr)

	// 操作数必须是指针类型
	if !d.Operand.GetBase().Type.IsPtr() {
		ctx.AddErrorf("解引用操作符 * 只能用于指针类型，实际为 %s", d.Operand.GetBase().Type)
		return nil, false
	}

	// 获取指针指向的类型
	elemType, ok := d.Operand.GetBase().Type.GetPtrElementType()
	if !ok {
		ctx.AddErrorf("无效的指针类型: %s", d.Operand.GetBase().Type)
		return nil, false
	}
	d.Type = elemType
	return d, true
}
