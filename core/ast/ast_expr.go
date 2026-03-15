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

func (c *IdentifierExpr) Check(ctx *SemanticContext) error {
	c.Name = c.Name.Resolve(&ctx.ValidContext)
	if !c.Name.Valid(&ctx.ValidContext) {
		return fmt.Errorf("invalid identifier: %s", c.Name)
	}
	vtp, b := ctx.GetVariable(c.Name)
	if !b {
		// 回退：检查是否为包别名
		if _, isPkg := ctx.root.Imports[string(c.Name)]; isPkg {
			c.Type = "Package"
			return nil
		}

		// 回退：尝试在当前包空间查找
		if !strings.Contains(string(c.Name), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
			mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
			if vt, ok := ctx.GetVariable(mangled); ok {
				c.Name = mangled // 关键：更新为转义后的名称
				c.Type = vt
				return nil
			}
		}

		return fmt.Errorf("变量 %s 不存在", c.Name)
	}

	// 特殊处理：如果是顶级变量且未带包名（同一包内访问），也补齐包名
	if !strings.Contains(string(c.Name), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
		if _, ok := ctx.root.vars[mangled]; ok {
			c.Name = mangled
		}
	}

	c.Type = vtp
	return nil
}

func (c *IdentifierExpr) Optimize(ctx *OptimizeContext) Node {
	return c
}

type ConstRefExpr struct {
	BaseNode
	Name Ident `json:"name"`
}

func (c *ConstRefExpr) GetBase() *BaseNode { return &c.BaseNode }
func (c *ConstRefExpr) exprNode()          {}

func (c *ConstRefExpr) Check(ctx *SemanticContext) error {
	c.Name = c.Name.Resolve(&ctx.ValidContext)
	if !c.Name.Valid(&ctx.ValidContext) {
		return fmt.Errorf("invalid identifier: %s", c.Name)
	}
	if vtp, b := ctx.GetVariable(c.Name); b {
		c.Type = vtp
		return nil
	}

	if vtp, b := ctx.GetFunction(c.Name); b {
		c.Type = vtp.MiniType()
		return nil
	}

	if ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
		if vtp, b := ctx.GetFunction(mangled); b {
			c.Name = mangled
			c.Type = vtp.MiniType()
			return nil
		}
	}

	if _, b := ctx.root.program.Constants[string(c.Name)]; b {
		c.Type = "Constant"
		return nil
	}

	if ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, c.Name))
		if _, b := ctx.root.program.Constants[string(mangled)]; b {
			c.Name = mangled
			c.Type = "Constant"
			return nil
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
			return nil
		}
	}

	return fmt.Errorf("const/function %s 不存在", c.Name)
}

func (c *ConstRefExpr) Optimize(ctx *OptimizeContext) Node {
	return c
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

func (c *CallExprStmt) Check(ctx *SemanticContext) error {
	if c.Func == nil {
		return fmt.Errorf("函数调用缺少函数名")
	}

	if err := c.Func.Check(ctx); err != nil {
		return err
	}

	fType, b := c.Func.GetBase().Type.ReadCallFunc()
	if !b {
		return fmt.Errorf("对象(%s)不是函数", c.Func.GetBase().Type)
	}

	for _, arg := range c.Args {
		if err := arg.Check(ctx); err != nil {
			return err
		}
	}

	// 语义校验：参数数量和基本类型匹配
	minParams := len(fType.Params)
	if fType.Variadic {
		minParams--
	}
	if len(c.Args) < minParams {
		return fmt.Errorf("函数参数数量不足: 需至少 %d, 实际 %d", minParams, len(c.Args))
	}
	if !fType.Variadic && len(fType.Params) > 0 && !fType.Params[len(fType.Params)-1].IsArray() && len(c.Args) > len(fType.Params) {
		return fmt.Errorf("函数参数数量过多: 需 %d, 实际 %d", len(fType.Params), len(c.Args))
	}

	// 校验固定参数部分的类型
	fixedNum := len(fType.Params)
	isImplicitArray := len(fType.Params) > 0 && fType.Params[len(fType.Params)-1].IsArray()
	if fType.Variadic || isImplicitArray {
		fixedNum--
	}

	for i := 0; i < fixedNum && i < len(c.Args); i++ {
		argType := c.Args[i].GetBase().Type
		if !fType.Params[i].Equals(argType) {
			if _, ok := fType.Params[i].AutoPtr(c.Args[i]); !ok {
				return fmt.Errorf("函数第 %d 个参数类型不匹配: 期望 %s, 实际 %s", i+1, fType.Params[i], argType)
			}
		}
	}

	// 校验隐式数组/变长参数的子项兼容性
	if isImplicitArray && len(c.Args) > fixedNum {
		targetArrayType := fType.Params[len(fType.Params)-1]
		targetElem, _ := targetArrayType.ReadArrayItemType()

		// 如果只有一个参数且正好是数组类型，视为完美匹配
		if len(c.Args) == fixedNum+1 {
			if targetArrayType.Equals(c.Args[fixedNum].GetBase().Type) {
				goto done
			}
		}

		for i := fixedNum; i < len(c.Args); i++ {
			argType := c.Args[i].GetBase().Type
			if !targetElem.Equals(argType) {
				if _, ok := targetElem.AutoPtr(c.Args[i]); !ok {
					return fmt.Errorf("函数变长参数部分第 %d 个元素类型不匹配: 期望 %s, 实际 %s", i-fixedNum+1, targetElem, argType)
				}
			}
		}
	}

done:
	c.Type = fType.Returns
	// 自动解包 (T, Error) 为 T
	if c.Type.IsTuple() {
		types, ok := c.Type.ReadTuple()
		if ok && len(types) == 2 && types[1] == "Error" {
			c.Type = types[0]
		}
	}

	return nil
}

func (c *CallExprStmt) Optimize(ctx *OptimizeContext) Node {
	c.Func = c.Func.Optimize(ctx).(Expr)
	for i, arg := range c.Args {
		c.Args[i] = arg.Optimize(ctx).(Expr)
	}

	fType, _ := c.Func.GetBase().Type.ReadCallFunc()

	// 变长参数支持 (Variadic Support)
	if fType.Variadic {
		paramCount := len(fType.Params)
		alreadyPacked := false
		if len(c.Args) == paramCount {
			lastArgType := c.Args[paramCount-1].GetBase().Type
			if lastArgType.IsArray() {
				alreadyPacked = true
			}
		}

		if !alreadyPacked && len(c.Args) >= paramCount-1 {
			fixedParams := fType.Params[:paramCount-1]
			variadicParam := fType.Params[paramCount-1]

			for i := 0; i < len(fixedParams); i++ {
				if ptr, ok := fixedParams[i].AutoPtr(c.Args[i]); ok {
					c.Args[i] = ptr
				}
			}

			variadicArgs := c.Args[paramCount-1:]
			targetElem, _ := variadicParam.ReadArrayItemType()

			wrappedElements := make([]CompositeElement, len(variadicArgs))
			for i, arg := range variadicArgs {
				if ptr, ok := targetElem.AutoPtr(arg); ok {
					wrappedElements[i] = CompositeElement{Value: ptr}
				} else {
					wrappedElements[i] = CompositeElement{Value: arg}
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
			return c
		}
	}

	// 隐式推导为数组参数
	if len(fType.Params) > 0 && fType.Params[len(fType.Params)-1].IsArray() {
		targetArrayType := fType.Params[len(fType.Params)-1]
		targetElem, _ := targetArrayType.ReadArrayItemType()

		isPerfectMatch := false
		if len(c.Args) == len(fType.Params) {
			if _, b2 := targetArrayType.AutoPtr(c.Args[len(fType.Params)-1]); b2 {
				isPerfectMatch = true
			}
		}

		if !isPerfectMatch && len(c.Args) >= len(fType.Params)-1 {
			for i := 0; i < len(fType.Params)-1; i++ {
				if ptr, ok := fType.Params[i].AutoPtr(c.Args[i]); ok {
					c.Args[i] = ptr
				}
			}

			variadicArgs := c.Args[len(fType.Params)-1:]
			wrappedElements := make([]CompositeElement, len(variadicArgs))
			for i, arg := range variadicArgs {
				if ptr, b2 := targetElem.AutoPtr(arg); b2 {
					wrappedElements[i] = CompositeElement{Value: ptr}
				} else {
					wrappedElements[i] = CompositeElement{Value: arg}
				}
			}

			c.Args = append(c.Args[:len(fType.Params)-1], &CompositeExpr{
				BaseNode: BaseNode{
					ID:   c.ID + "_ArgsWrap",
					Meta: "composite",
					Type: targetArrayType,
				},
				Kind:   Ident(targetArrayType),
				Values: wrappedElements,
			})
			return c
		}
	}

	for i, param := range fType.Params {
		if i < len(c.Args) {
			arg := tryAutoNumericCast(ctx.ValidContext, param, c.Args[i])
			if ptr, b2 := param.AutoPtr(arg); b2 {
				c.Args[i] = ptr
			} else {
				c.Args[i] = arg
			}
		}
	}

	return c
}

// MemberExpr 表示成员访问表达式 (a.b)
type MemberExpr struct {
	BaseNode
	Object   Expr  `json:"object"`
	Property Ident `json:"property"`
}

func (m *MemberExpr) GetBase() *BaseNode { return &m.BaseNode }
func (m *MemberExpr) exprNode()          {}

func (m *MemberExpr) Check(ctx *SemanticContext) error {
	if m.Object == nil {
		return fmt.Errorf("成员访问缺少对象表达式")
	}
	if m.Property == "" {
		return fmt.Errorf("成员访问缺少属性名")
	}

	// 1. Package selector check (Static Inlining detection)
	if ident, ok := m.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			firstChar := string(m.Property)[0]
			if firstChar < 'A' || firstChar > 'Z' {
				return fmt.Errorf("cannot refer to unexported name %s.%s", ident.Name, m.Property)
			}
			// It's a package selector, we'll transform it in Optimize
			// For now, we need to determine its type
			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}
			mangledName := fmt.Sprintf("%s.%s", pkgName, m.Property)
			// Check if it exists as a function or variable in that package
			if vtp, b := ctx.GetVariable(Ident(mangledName)); b {
				m.Type = vtp
				return nil
			}
			if vtp, b := ctx.GetFunction(Ident(mangledName)); b {
				m.Type = vtp.MiniType()
				return nil
			}
			return fmt.Errorf("package member %s not found", mangledName)
		}
	}

	if err := m.Object.Check(ctx); err != nil {
		return err
	}

	if !m.Property.Valid(&ctx.ValidContext) {
		return fmt.Errorf("invalid property: %s", m.Property)
	}

	name, b := m.Object.GetBase().Type.StructName()
	if !b {
		return fmt.Errorf("不是合法的 struct (%s)", m.Object.GetBase().Type)
	}

	miniStruct, b := ctx.GetStruct(name)
	if !b {
		return fmt.Errorf("未定义 struct (%s)", name)
	}

	met, b := miniStruct.Fields[m.Property]
	if !b {
		if method, ok := miniStruct.Methods[m.Property]; ok {
			met = method.MiniType()
		} else {
			return fmt.Errorf("struct(%s) 不存在 (%s)", name, m.Property)
		}
	}

	m.Type = met
	return nil
}

func (m *MemberExpr) Optimize(ctx *OptimizeContext) Node {
	// 1. Package selector transformation
	if ident, ok := m.Object.(*IdentifierExpr); ok {
		if realPkg, isPkg := ctx.root.Imports[string(ident.Name)]; isPkg {
			pkgName := ctx.root.PathToPackage[realPkg]
			if pkgName == "" {
				pkgName = realPkg
			}
			mangledName := fmt.Sprintf("%s.%s", pkgName, m.Property)
			constRef := &ConstRefExpr{
				BaseNode: BaseNode{
					ID:   m.ID,
					Meta: "const_ref",
					Type: m.Type,
				},
				Name: Ident(mangledName),
			}
			return constRef.Optimize(ctx)
		}
	}

	m.Object = m.Object.Optimize(ctx).(Expr)
	return m
}

// CompositeExpr 表示复合类型表达式
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

func (c *CompositeExpr) Check(ctx *SemanticContext) error {
	c.Kind = Ident(GoMiniType(c.Kind).Resolve(&ctx.ValidContext))
	c.Type = GoMiniType(c.Kind)
	if c.Kind == "" {
		return fmt.Errorf("复合类型缺少类型标识")
	}
	for _, elem := range c.Values {
		if elem.Key != nil {
			if err := elem.Key.Check(ctx); err != nil {
				return err
			}
		}
		if elem.Value == nil {
			return fmt.Errorf("复合类型元素缺少值")
		}
		if err := elem.Value.Check(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (c *CompositeExpr) Optimize(ctx *OptimizeContext) Node {
	for i, elem := range c.Values {
		if elem.Key != nil {
			c.Values[i].Key = elem.Key.Optimize(ctx).(Expr)
		}
		c.Values[i].Value = elem.Value.Optimize(ctx).(Expr)
	}
	return c
}

// IndexExpr 表示索引访问表达式 i[1]
type IndexExpr struct {
	BaseNode
	Object Expr `json:"object"`
	Index  Expr `json:"index"`
}

func (i *IndexExpr) GetBase() *BaseNode { return &i.BaseNode }
func (i *IndexExpr) exprNode()          {}

func (i *IndexExpr) Check(ctx *SemanticContext) error {
	if i.Object == nil {
		return fmt.Errorf("索引访问缺少对象表达式")
	}

	if err := i.Object.Check(ctx); err != nil {
		return err
	}

	if i.Index == nil {
		return fmt.Errorf("索引访问缺少索引表达式")
	}

	if err := i.Index.Check(ctx); err != nil {
		return err
	}

	objType := i.Object.GetBase().Type
	if objType.IsPtr() {
		if elem, ok := objType.GetPtrElementType(); ok {
			objType = elem
		}
	}

	if objType.IsArray() {
		if i.Index.GetBase().Type != "Int64" {
			return fmt.Errorf("数组索引只支持 Int64 类型 (%s)", i.Index.GetBase().Type)
		}

		if elemType, ok := objType.ReadArrayItemType(); ok {
			i.Type = elemType
		} else {
			return fmt.Errorf("无法获取数组元素类型: %s", objType)
		}
		return nil
	}
	if objType.IsMap() {
		keyType, valType, ok := objType.GetMapKeyValueTypes()
		if !ok {
			return fmt.Errorf("无法获取Map类型信息: %s", objType)
		}
		if !keyType.Equals(i.Index.GetBase().Type) {
			return fmt.Errorf("Map索引类型不匹配: 需 %s, 实际 %s", keyType, i.Index.GetBase().Type)
		}
		i.Type = valType
		return nil
	}
	return fmt.Errorf("索引访问的对象必须是数组或Map类型，实际为 %s", objType)
}

func (i *IndexExpr) Optimize(ctx *OptimizeContext) Node {
	i.Object = i.Object.Optimize(ctx).(Expr)
	i.Index = i.Index.Optimize(ctx).(Expr)
	return i
}

// AddressExpr 表示取地址表达式 &x
type AddressExpr struct {
	BaseNode
	Operand Expr `json:"operand"`
}

func (a *AddressExpr) GetBase() *BaseNode { return &a.BaseNode }
func (a *AddressExpr) exprNode()          {}

func (a *AddressExpr) Check(ctx *SemanticContext) error {
	if a.Operand == nil {
		return fmt.Errorf("取地址表达式缺少操作数")
	}
	if err := a.Operand.Check(ctx); err != nil {
		return err
	}
	if a.Operand.GetBase().Type.IsPtr() {
		return fmt.Errorf("取地址操作符 & 只能用于左值")
	}
	a.Type = a.Operand.GetBase().Type.ToPtr()
	return nil
}

func (a *AddressExpr) Optimize(ctx *OptimizeContext) Node {
	a.Operand = a.Operand.Optimize(ctx).(Expr)
	return a
}

// DerefExpr 表示解引用表达式 *p
type DerefExpr struct {
	BaseNode
	Operand Expr `json:"operand"`
}

func (d *DerefExpr) GetBase() *BaseNode { return &d.BaseNode }
func (d *DerefExpr) exprNode()          {}

func (d *DerefExpr) Check(ctx *SemanticContext) error {
	if d.Operand == nil {
		return fmt.Errorf("解引用表达式缺少操作数")
	}

	if err := d.Operand.Check(ctx); err != nil {
		return err
	}

	if !d.Operand.GetBase().Type.IsPtr() {
		return fmt.Errorf("解引用操作符 * 只能用于指针类型，实际为 %s", d.Operand.GetBase().Type)
	}

	elemType, ok := d.Operand.GetBase().Type.GetPtrElementType()
	if !ok {
		return fmt.Errorf("无效的指针类型: %s", d.Operand.GetBase().Type)
	}
	d.Type = elemType
	return nil
}

func (d *DerefExpr) Optimize(ctx *OptimizeContext) Node {
	d.Operand = d.Operand.Optimize(ctx).(Expr)
	return d
}
