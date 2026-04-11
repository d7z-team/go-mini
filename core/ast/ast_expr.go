package ast

import (
	"errors"
	"fmt"
	"strings"
)

func invalidReason(node Node, fallback string) string {
	if node == nil || node.GetBase() == nil || !node.GetBase().IsInvalid() {
		return fallback
	}
	if cause := strings.TrimSpace(node.GetBase().InvalidCause); cause != "" {
		return cause
	}
	meta := node.GetBase().Meta
	if meta == "" {
		meta = "表达式"
	}
	return fmt.Sprintf("前置%s存在错误，无法精确推导", meta)
}

func compositeInvalidCause(kind string, index int, child Node) string {
	ordinal := index + 1
	suffix := "值"
	if kind == "key" {
		suffix = "键"
	}
	base := fmt.Sprintf("复合字面量第 %d 个元素的%s存在错误，无法精确推导", ordinal, suffix)
	childCause := invalidReason(child, "")
	if childCause == "" || childCause == base {
		return base
	}
	return fmt.Sprintf("%s: %s", base, childCause)
}

type IdentifierExpr struct {
	BaseNode
	Name Ident `json:"name"`
}

func (c *IdentifierExpr) GetBase() *BaseNode { return &c.BaseNode }
func (c *IdentifierExpr) exprNode()          {}

func (c *IdentifierExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(c)
	c.Name = c.Name.Resolve(ctx.ValidContext)
	if !c.Name.Valid(ctx.ValidContext) {
		return fmt.Errorf("invalid identifier: %s", c.Name)
	}

	if c.IsType {
		// 处于类型上下文，将其视为类型名
		// 特殊处理 nil
		if c.Name == "nil" {
			c.Type = "nil"
			return nil
		}
		c.Type = GoMiniType(c.Name)
		return nil
	}

	vtp, b := ctx.GetVariable(c.Name)
	if !b {
		// 回退：检查是否为包别名
		if _, isPkg := ctx.root.Imports[string(c.Name)]; isPkg {
			c.Type = "Package"
			return nil
		}

		err := fmt.Errorf("变量 %s 不存在", c.Name)
		ctx.WithNode(c).AddErrorf("%s", err.Error())
		return err
	}

	c.Type = vtp
	return nil
}

func (c *IdentifierExpr) Optimize(ctx *OptimizeContext) Node {
	return c
}

// StarExpr 表示解引用表达式 (*p)
type StarExpr struct {
	BaseNode
	X Expr `json:"x"`
}

func (s *StarExpr) GetBase() *BaseNode { return &s.BaseNode }
func (s *StarExpr) exprNode()          {}

func (s *StarExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(s)
	if s.X == nil {
		err := errors.New("解引用缺少对象")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if err := s.X.Check(ctx.WithNode(s.X)); err != nil {
		return err
	}
	xType := s.X.GetBase().Type
	if xType.IsPtr() {
		if elem, ok := xType.GetPtrElementType(); ok {
			s.Type = elem
		} else {
			s.Type = "Any"
		}
	} else if xType.IsAny() {
		if s.X.GetBase().IsInvalid() {
			err := errors.New(invalidReason(s.X, "前置表达式存在错误，无法精确推导解引用结果"))
			ctx.AddErrorf("%s", err.Error())
			s.InvalidCause = err.Error()
			return err
		}
		s.Type = "Any"
	} else {
		err := fmt.Errorf("无法解引用非指针类型: %s", xType)
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	return nil
}

func (s *StarExpr) Optimize(ctx *OptimizeContext) Node {
	if s.X != nil {
		if opt := s.X.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				s.X = val
			}
		}
	}
	return s
}

type ConstRefExpr struct {
	BaseNode
	Name Ident `json:"name"`
}

func (c *ConstRefExpr) GetBase() *BaseNode { return &c.BaseNode }
func (c *ConstRefExpr) exprNode()          {}

// TypeAssertExpr 表示类型断言表达式 x.(Type)
type TypeAssertExpr struct {
	BaseNode
	X     Expr       `json:"x"`
	Type  GoMiniType `json:"assert_type"`
	Multi bool       `json:"multi,omitempty"` // 为 true 时返回 (val, ok) Tuple
}

func (t *TypeAssertExpr) GetBase() *BaseNode { return &t.BaseNode }
func (t *TypeAssertExpr) exprNode()          {}

func (t *TypeAssertExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(t)
	if t.X == nil {
		err := errors.New("类型断言缺少对象")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if err := t.X.Check(ctx.WithNode(t.X)); err != nil {
		return err
	}
	if !t.Type.Valid(ctx.ValidContext) {
		err := fmt.Errorf("无效的断言类型: %s", t.Type)
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	// 断言后的类型即为目标类型
	if t.Multi {
		t.BaseNode.Type = CreateTupleType(t.Type, "Bool")
	} else {
		t.BaseNode.Type = t.Type
	}
	return nil
}

func (t *TypeAssertExpr) Optimize(ctx *OptimizeContext) Node {
	if t.X != nil {
		if opt := t.X.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				t.X = val
			}
		}
	}
	return t
}

func (c *ConstRefExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(c)
	c.Name = c.Name.Resolve(ctx.ValidContext)
	if !c.Name.Valid(ctx.ValidContext) {
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

	if _, b := ctx.root.program.Constants[string(c.Name)]; b {
		c.Type = "Constant"
		return nil
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

	err := fmt.Errorf("const/function %s 不存在", c.Name)
	ctx.AddErrorf("%s", err.Error())
	return err
}

func (c *ConstRefExpr) Optimize(ctx *OptimizeContext) Node {
	return c
}

// CallExprStmt 表示函数调用表达式
type CallExprStmt struct {
	BaseNode
	Func     Expr   `json:"func"`               // 被调用表达式
	Args     []Expr `json:"args"`               // 调用参数
	Ellipsis bool   `json:"ellipsis,omitempty"` // 为 true 时表示 f(args...)
}

func (c *CallExprStmt) GetBase() *BaseNode { return &c.BaseNode }
func (c *CallExprStmt) exprNode()          {}
func (c *CallExprStmt) stmtNode()          {}

func (c *CallExprStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(c)
	if c.Func == nil {
		err := errors.New("函数调用缺少函数名")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	// 此时错误已经由 c.Func.Check 内部通过自己的 context 报告了
	funcErr := c.Func.Check(ctx.WithNode(c.Func))

	// 无论函数名是否能解析成功，我们都必须校验参数，以便填充 LSP 所需的类型信息

	// 特殊处理 make/new 的类型参数解析
	if ident, ok := c.Func.(*ConstRefExpr); ok && (ident.Name == "make" || ident.Name == "new") {
		if len(c.Args) > 0 {
			if lit, ok2 := c.Args[0].(*LiteralExpr); ok2 && lit.Type == "String" {
				t := GoMiniType(lit.Value).Resolve(ctx.ValidContext)
				if ident.Name == "make" {
					if !t.IsStrictValid() {
						err := fmt.Errorf("make: 非法类型 %s", lit.Value)
						ctx.AddErrorf("%s", err.Error())
						return err
					}
				} else { // new
					if !t.IsStrictValid() {
						if _, hasStruct := ctx.GetStruct(Ident(t)); !hasStruct {
							err := fmt.Errorf("new: 非法类型 %s", lit.Value)
							ctx.AddErrorf("%s", err.Error())
							return err
						}
					}
					t = t.ToPtr()
				}
				lit.Value = string(t)
			} else {
				// 如果不是字面量字符串，说明是动态变量，报错
				err := fmt.Errorf("%s: 第一个参数必须是表示类型的字符串字面量", ident.Name)
				ctx.AddErrorf("%s", err.Error())
				return err
			}
		}
	}

	for _, arg := range c.Args {
		if err := arg.Check(ctx.WithNode(arg)); err != nil {
			funcErr = err
		}
	}

	if funcErr != nil {
		return funcErr
	}

	fType, b := c.Func.GetBase().Type.ReadCallFunc()
	if !b {
		if c.Func.GetBase().Type.IsAny() {
			if c.Func.GetBase().IsInvalid() {
				err := errors.New(invalidReason(c.Func, "调用目标存在错误，无法精确推导返回类型"))
				ctx.AddErrorf("%s", err.Error())
				c.InvalidCause = err.Error()
				return err
			}
			c.Type = "Any"
			return nil
		}
		err := fmt.Errorf("对象(%s)不是函数", c.Func.GetBase().Type)
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	// 语义校验：参数数量和基本类型匹配
	sigParams := fType.Params
	minParams := len(sigParams)
	if fType.Variadic {
		minParams--
	}

	// 校验固定参数部分的类型
	fixedNum := len(sigParams)
	isImplicitArray := len(sigParams) > 0 && sigParams[len(sigParams)-1].IsArray()
	if fType.Variadic || isImplicitArray {
		fixedNum--
	}

	if c.Ellipsis {
		// 如果使用了 f(args...)，则参数数量必须固定为 1 (针对变长参数函数)
		// 或者符合被调用函数的参数结构
		if len(c.Args) == 0 {
			err := errors.New("invalid use of ellipsis with no arguments")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		// 校验最后一个参数必须是数组类型
		lastArgType := c.Args[len(c.Args)-1].GetBase().Type
		if !lastArgType.IsArray() && !lastArgType.IsAny() {
			err := fmt.Errorf("invalid use of ellipsis with non-array type %s", lastArgType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		goto done // 变长参数展开跳过常规数量检查，运行时处理
	}

	if len(c.Args) < minParams {
		err := fmt.Errorf("函数参数数量不足: 需至少 %d, 实际 %d", minParams, len(c.Args))
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if !fType.Variadic && !isImplicitArray && len(sigParams) > 0 && len(c.Args) > len(sigParams) {
		err := fmt.Errorf("函数参数数量过多: 需 %d, 实际 %d", len(sigParams), len(c.Args))
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	for i := 0; i < fixedNum && i < len(c.Args); i++ {
		argType := c.Args[i].GetBase().Type
		if !argType.IsAssignableTo(sigParams[i]) {
			err := fmt.Errorf("函数第 %d 个参数类型不匹配: 期望 %s, 实际 %s", i+1, sigParams[i], argType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	}

	// 校验隐式数组/变长参数的子项兼容性
	if isImplicitArray && len(c.Args) > fixedNum {
		targetArrayType := sigParams[len(sigParams)-1]
		targetElem, _ := targetArrayType.ReadArrayItemType()

		// 如果只有一个参数且正好是数组类型，视为完美匹配
		if len(c.Args) == fixedNum+1 {
			if c.Args[fixedNum].GetBase().Type.IsAssignableTo(targetArrayType) {
				goto done
			}
		}

		for i := fixedNum; i < len(c.Args); i++ {
			argType := c.Args[i].GetBase().Type
			if !argType.IsAssignableTo(targetElem) {
				err := fmt.Errorf("函数变长参数部分第 %d 个元素类型不匹配: 期望 %s, 实际 %s", i-fixedNum+1, targetElem, argType)
				ctx.AddErrorf("%s", err.Error())
				return err
			}
		}
	}

done:
	c.Type = fType.Returns
	// 特殊处理内建函数的返回类型推导
	if ident, ok := c.Func.(*ConstRefExpr); ok {
		switch ident.Name {
		case "make", "new":
			if len(c.Args) > 0 {
				if lit, ok2 := c.Args[0].(*LiteralExpr); ok2 && lit.Type == "String" {
					c.Type = GoMiniType(lit.Value)
				}
			}
		case "append":
			if len(c.Args) > 0 {
				// append 返回第一个参数的类型 (通常是 Array<T>)
				c.Type = c.Args[0].GetBase().Type
			}
		case "len", "cap":
			c.Type = "Int64"
			if len(c.Args) > 0 {
				argType := c.Args[0].GetBase().Type
				if !argType.IsArray() && !argType.IsMap() && argType != "String" && argType != "TypeBytes" && !argType.IsAny() {
					err := fmt.Errorf("%s: 不支持类型 %s", ident.Name, argType)
					ctx.AddErrorf("%s", err.Error())
					return err
				}
			}
		}
	}

	return nil
}

func (c *CallExprStmt) Optimize(ctx *OptimizeContext) Node {
	if c.Func != nil {
		if opt := c.Func.Optimize(ctx); opt != nil {
			c.Func = opt.(Expr)
		}
	}
	for i, arg := range c.Args {
		if arg != nil {
			if opt := arg.Optimize(ctx); opt != nil {
				c.Args[i] = opt.(Expr)
			}
		}
	}

	if c.Func != nil && c.Func.GetBase() != nil {
		fType, ok := c.Func.GetBase().Type.ReadCallFunc()
		if ok && fType != nil {
			sigParams := fType.Params
			for i, param := range sigParams {
				if i < len(c.Args) && c.Args[i] != nil {
					arg := c.Args[i]
					if ptr, b2 := param.AutoPtr(arg); b2 {
						c.Args[i] = ptr
					} else {
						c.Args[i] = arg
					}
				}
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
		return errors.New("成员访问缺少对象表达式")
	}
	if m.Property == "" {
		return errors.New("成员访问缺少属性名")
	}

	discoveredPackage := false
	if id, ok := m.Object.(*IdentifierExpr); ok {
		if _, known, imported := ctx.root.ResolvePackage(id.Name); known && !imported {
			id.Type = "Package"
			discoveredPackage = true
		}
	}

	if !discoveredPackage {
		if err := m.Object.Check(ctx.WithNode(m.Object)); err != nil {
			return err
		}
	}

	objType := m.Object.GetBase().Type
	if objType == "Any" {
		if m.Object.GetBase().IsInvalid() {
			err := errors.New(invalidReason(m.Object, "成员访问对象存在错误，无法精确推导成员类型"))
			ctx.WithNode(m).AddErrorf("%s", err.Error())
			m.InvalidCause = err.Error()
			return err
		}
		m.Type = "Any"
		return nil
	}

	if objType == "Error" {
		if m.Property == "Error" {
			m.Type = "function() String"
			return nil
		}
	}

	if objType == "Package" || objType == TypeModule {
		if id, ok := m.Object.(*IdentifierExpr); ok {
			path, knownPkg, explicitlyImported := ctx.root.ResolvePackage(id.Name)
			reportMissingImport := func() {
				if !explicitlyImported {
					ctx.WithNode(m).AddErrorf("包 %s 已解析但未导入", id.Name)
				}
			}
			if !knownPkg {
				path = string(id.Name)
				// 尝试查找后缀匹配的 ImportedRoot
				for fullPath := range ctx.root.ImportedRoots {
					if fullPath == path || strings.HasSuffix(fullPath, "/"+path) {
						path = fullPath
						break
					}
				}
			}

			// 尝试从 ImportedRoots 中直接获取成员 (Go-source 模块)
			if srcRoot, ok := ctx.root.ImportedRoots[path]; ok {
				reportMissingImport()
				prop := string(m.Property)
				// 1. 变量/函数
				if t, ok := srcRoot.vars[Ident(prop)]; ok {
					m.Type = t
					return nil
				}
				// 2. 结构体
				if _, ok := srcRoot.structs[Ident(prop)]; ok {
					m.Type = GoMiniType(prop) // 或者需要包含包路径?
					return nil
				}
				// 3. 接口
				if _, ok := srcRoot.interfaces[Ident(prop)]; ok {
					m.Type = GoMiniType(prop)
					return nil
				}
			}

			// 尝试多种路径格式
			// 1. 原始路径 (Go-source 模块使用 /)
			p1 := Ident(path + "." + string(m.Property))
			// 2. FFI 风格路径 (FFI 标准库将 / 映射为 .)
			p2 := Ident(strings.ReplaceAll(path, "/", ".") + "." + string(m.Property))

			targets := []Ident{p1}
			if p1 != p2 {
				targets = append(targets, p2)
			}

			for _, fullPath := range targets {
				// 1. 尝试作为变量/常量查找
				if t, ok := ctx.GetVariable(fullPath); ok {
					reportMissingImport()
					m.Type = t
					return nil
				}
				// 2. 尝试作为函数查找
				if fn, ok := ctx.GetFunction(fullPath); ok {
					reportMissingImport()
					m.Type = fn.MiniType()
					return nil
				}
				// 3. 尝试作为结构体查找
				if _, ok := ctx.GetStruct(fullPath); ok {
					reportMissingImport()
					m.Type = GoMiniType(fullPath)
					return nil
				}
				// 4. 尝试作为接口查找
				if _, ok := ctx.GetInterface(fullPath); ok {
					reportMissingImport()
					m.Type = GoMiniType(fullPath)
					return nil
				}
			}

			err := fmt.Errorf("包 %s 不存在成员 %s", id.Name, m.Property)
			ctx.WithNode(m).AddErrorf("%s", err.Error())
			return err
		}
		err := errors.New("成员访问对象无法解析为包或结构体")
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		m.InvalidCause = err.Error()
		return err
	}

	if objType.IsInterface() {
		methods, _ := objType.ReadInterfaceMethods()
		if sig, ok := methods[string(m.Property)]; ok {
			m.Type = sig.MiniType() // 使用解析出的完整签名类型
			return nil
		}
		err := fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		return err
	}

	// 检查是否为命名接口
	if iStmt, ok := ctx.GetInterface(Ident(objType)); ok {
		methods, _ := iStmt.Type.ReadInterfaceMethods()
		if sig, ok := methods[string(m.Property)]; ok {
			m.Type = sig.MiniType()
			return nil
		}
		err := fmt.Errorf("interface %s does not support member access to %s", objType, m.Property)
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		return err
	}

	if objType.IsMap() {
		_, vType, ok := objType.GetMapKeyValueTypes()
		if ok {
			if m.Object.GetBase().IsInvalid() && vType.IsAny() {
				err := errors.New(invalidReason(m.Object, "成员访问对象存在错误，无法精确推导成员类型"))
				ctx.WithNode(m).AddErrorf("%s", err.Error())
				m.InvalidCause = err.Error()
				return err
			}
			m.Type = vType
			return nil
		}
	}

	if objType.IsAny() {
		m.Type = "Any"
		return nil
	}

	if objType.IsPrimitive() {
		err := fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		return err
	}

	if objType.IsArray() {
		err := fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		return err
	}

	if !m.Property.Valid(ctx.ValidContext) {
		err := fmt.Errorf("invalid property: %s", m.Property)
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		return err
	}

	// 尝试作为结构体访问
	typeName := objType.BaseName()
	if typeName != "" {
		miniStruct, b := ctx.GetStruct(Ident(typeName))
		if b {
			met, b := miniStruct.Fields[m.Property]
			if !b {
				if method, ok := miniStruct.Methods[m.Property]; ok {
					sig := method
					// 对于结构体方法，第一个参数通常是接收者。
					// 如果是通过对象访问（不是包名），我们需要剥离接收者以便支持方法值和简化调用校验。
					if objType != "Package" && objType != TypeModule {
						if len(sig.Params) > 0 && structMethodHasReceiver(sig, objType) {
							sig.Params = sig.Params[1:]
						}
					}
					m.Type = sig.MiniType()
					return nil
				}
				// 找到了结构体但没找到字段和方法，报错
				err := fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
				ctx.WithNode(m).AddErrorf("%s", err.Error())
				return err
			}
			m.Type = met
			return nil
		}
		// 结构体未在当前上下文中定义。
		// 如果是内置类型（非 Package/Any），但又没找到 Struct 定义，说明确实不支持。
		// 对于 FFI 宿主类型或跨包类型，通常我们会把它们注册为 Package 或 Any 或者是特定的命名的 Type。
		// 如果走到这里，说明 typeName 不是 Package/Any 却没定义。
		// 如果不是宽容模式，或者不是跨包/FFI 类型，默认放行可能不安全。

		// 如果 typeName 包含点号，说明是跨包类型，且当前环境中没有加载该包的 AST，
		// 或者它是一个 FFI 类型。在这种情况下，我们保持 Any 类型。
		if strings.Contains(typeName, ".") {
			err := fmt.Errorf("未解析的跨包类型 %s，无法访问成员 %s", objType, m.Property)
			ctx.WithNode(m).AddErrorf("%s", err.Error())
			return err
		}

		// 如果 objType 包含点号，也说明是跨包类型（例如 lib.Point）
		if strings.Contains(string(objType), ".") {
			err := fmt.Errorf("未解析的跨包类型 %s，无法访问成员 %s", objType, m.Property)
			ctx.WithNode(m).AddErrorf("%s", err.Error())
			return err
		}

		err := fmt.Errorf("未定义类型 %s，无法访问成员 %s", objType, m.Property)
		ctx.WithNode(m).AddErrorf("%s", err.Error())
		return err
	}

	err := fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
	ctx.WithNode(m).AddErrorf("%s", err.Error())
	return err
}

func structMethodHasReceiver(sig CallFunctionType, objType GoMiniType) bool {
	if len(sig.Params) == 0 {
		return false
	}
	first := sig.Params[0]
	objBase := GoMiniType(objType.BaseName())
	firstBase := GoMiniType(first.BaseName())
	if objBase == "" || firstBase == "" {
		return false
	}
	return objBase == firstBase
}

func (m *MemberExpr) Optimize(ctx *OptimizeContext) Node {
	if m.Object != nil {
		if opt := m.Object.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				m.Object = val
			}
		}
	}
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
	ctx = ctx.WithNode(c)
	invalidCause := ""
	if c.Kind != "" {
		c.Kind = Ident(GoMiniType(c.Kind).Resolve(ctx.ValidContext))
		c.Type = GoMiniType(c.Kind)
	}

	if c.Type == "" {
		// 尝试从 BaseNode 获取（可能由外层 Check 预设）
		c.Type = c.BaseNode.Type
	}

	if c.Type == "" || c.Type == "Any" {
		// 最后的启发式：根据是否有 Key 判定是 Array 还是 Map，并尝试细化元素类型
		hasKey := false
		var commonValType GoMiniType
		var commonKeyType GoMiniType
		allSameVal := true
		allSameKey := true

		for i, v := range c.Values {
			if v.Key != nil {
				hasKey = true
				if err := v.Key.Check(ctx); err == nil {
					if v.Key.GetBase().IsInvalid() {
						if invalidCause == "" {
							invalidCause = compositeInvalidCause("key", i, v.Key)
						}
					}
					kt := v.Key.GetBase().Type
					if i == 0 {
						commonKeyType = kt
					} else if !kt.Equals(commonKeyType) {
						allSameKey = false
					}
				} else {
					if invalidCause == "" {
						invalidCause = compositeInvalidCause("key", i, v.Key)
					}
				}
			} else {
				allSameKey = false
			}

			if v.Value != nil {
				if err := v.Value.Check(ctx); err == nil {
					if v.Value.GetBase().IsInvalid() {
						if invalidCause == "" {
							invalidCause = compositeInvalidCause("value", i, v.Value)
						}
					}
					vt := v.Value.GetBase().Type
					if i == 0 {
						commonValType = vt
					} else if !vt.Equals(commonValType) {
						allSameVal = false
					}
				} else {
					if invalidCause == "" {
						invalidCause = compositeInvalidCause("value", i, v.Value)
					}
				}
			}
		}

		if hasKey {
			finalKey := GoMiniType("String")
			if allSameKey && !commonKeyType.IsEmpty() {
				finalKey = commonKeyType
			}
			finalVal := GoMiniType("Any")
			if allSameVal && !commonValType.IsEmpty() {
				finalVal = commonValType
			}
			c.Type = GoMiniType(fmt.Sprintf("Map<%s, %s>", finalKey, finalVal))
		} else {
			finalVal := GoMiniType("Any")
			if allSameVal && !commonValType.IsEmpty() && len(c.Values) > 0 {
				finalVal = commonValType
			}
			c.Type = CreateArrayType(finalVal)
		}
	}

	isMap := c.Type.IsMap()
	isArray := c.Type.IsArray()

	var elemType GoMiniType
	var keyType GoMiniType
	var valType GoMiniType

	if isArray {
		elemType, _ = c.Type.ReadArrayItemType()
	} else if isMap {
		keyType, valType, _ = c.Type.GetMapKeyValueTypes()
	}

	var miniStruct *ValidStruct
	var hasStruct bool
	if !isMap && !isArray && c.Kind != "" {
		miniStruct, hasStruct = ctx.GetStruct(c.Kind)
	}

	for idx, elem := range c.Values {
		if elem.Key != nil {
			if isMap {
				if sub, ok := elem.Key.(*CompositeExpr); ok && sub.Kind == "" {
					sub.BaseNode.Type = keyType
				}
			} else if !isArray {
				// 结构体 Key 必须是字段名
				if ident, ok := elem.Key.(*IdentifierExpr); ok {
					if hasStruct {
						if fieldType, ok2 := miniStruct.Fields[ident.Name]; ok2 {
							// 记录字段类型以便后续 Value 校验
							if sub, ok3 := elem.Value.(*CompositeExpr); ok3 && sub.Kind == "" {
								sub.BaseNode.Type = fieldType
							}
						} else {
							return fmt.Errorf("结构体 %s 不存在字段 %s", c.Kind, ident.Name)
						}
					}
					ident.Type = "Any"
					goto checkValue
				}
			}
			if err := elem.Key.Check(ctx); err != nil {
				return err
			}
			if elem.Key.GetBase().IsInvalid() {
				if invalidCause == "" {
					invalidCause = compositeInvalidCause("key", idx, elem.Key)
				}
			}
		}

	checkValue:
		if elem.Value == nil {
			return errors.New("复合类型元素缺少值")
		}
		// 预设子元素类型
		if sub, ok := elem.Value.(*CompositeExpr); ok && sub.Kind == "" {
			if isArray {
				sub.BaseNode.Type = elemType
			} else if isMap {
				sub.BaseNode.Type = valType
			}
		}
		if err := elem.Value.Check(ctx); err != nil {
			return err
		}
		if elem.Value.GetBase().IsInvalid() {
			if invalidCause == "" {
				invalidCause = compositeInvalidCause("value", idx, elem.Value)
			}
		}
	}

	c.InvalidCause = invalidCause

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
	Multi  bool `json:"multi,omitempty"` // 为 true 时返回 (val, ok) Tuple
}

func (i *IndexExpr) GetBase() *BaseNode { return &i.BaseNode }
func (i *IndexExpr) exprNode()          {}

func (i *IndexExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(i)
	if i.Object == nil {
		err := errors.New("索引访问缺少对象表达式")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	if err := i.Object.Check(ctx.WithNode(i.Object)); err != nil {
		return err
	}

	if i.Index == nil {
		err := errors.New("索引访问缺少索引表达式")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	if err := i.Index.Check(ctx.WithNode(i.Index)); err != nil {
		return err
	}

	objType := i.Object.GetBase().Type

	if i.Object.GetBase().IsInvalid() && objType.IsAny() {
		err := errors.New(invalidReason(i.Object, "索引对象存在前置错误，无法精确推导索引结果"))
		ctx.AddErrorf("%s", err.Error())
		i.InvalidCause = err.Error()
		return err
	}
	if i.Index.GetBase().IsInvalid() && i.Index.GetBase().Type.IsAny() {
		err := errors.New(invalidReason(i.Index, "索引表达式存在前置错误，无法精确推导索引结果"))
		ctx.AddErrorf("%s", err.Error())
		i.InvalidCause = err.Error()
		return err
	}

	if objType.IsAny() {
		if i.Multi {
			i.Type = CreateTupleType("Any", "Bool")
		} else {
			i.Type = "Any"
		}
		return nil
	}

	if objType.IsPtr() {
		if elem, ok := objType.GetPtrElementType(); ok {
			objType = elem
		}
	}

	if objType.IsArray() {
		if i.Multi {
			err := errors.New("数组索引不支持二元解构语法")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if i.Index.GetBase().Type != "Int64" {
			err := fmt.Errorf("数组索引只支持 Int64 类型 (%s)", i.Index.GetBase().Type)
			ctx.AddErrorf("%s", err.Error())
			return err
		}

		if elemType, ok := objType.ReadArrayItemType(); ok {
			if i.Object.GetBase().IsInvalid() && elemType.IsAny() {
				err := errors.New(invalidReason(i.Object, "索引对象存在错误，无法精确推导索引结果"))
				ctx.AddErrorf("%s", err.Error())
				i.InvalidCause = err.Error()
				return err
			}
			i.Type = elemType
		} else {
			err := fmt.Errorf("无法获取数组元素类型: %s", objType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		return nil
	}
	if objType == "TypeBytes" {
		if i.Multi {
			err := errors.New("Bytes 索引不支持二元解构语法")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if i.Index.GetBase().Type != "Int64" {
			err := fmt.Errorf("Bytes 索引只支持 Int64 类型 (%s)", i.Index.GetBase().Type)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		i.Type = "Int64"
		return nil
	}
	if objType == "String" {
		if i.Multi {
			err := errors.New("String 索引不支持二元解构语法")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if i.Index.GetBase().Type != "Int64" {
			err := fmt.Errorf("String 索引只支持 Int64 类型 (%s)", i.Index.GetBase().Type)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		i.Type = "Int64" // 返回字节值
		return nil
	}
	if objType.IsMap() {
		keyType, valType, ok := objType.GetMapKeyValueTypes()
		if !ok {
			err := fmt.Errorf("无法获取 Map 键值对类型: %s", objType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if i.Index.GetBase().Type.IsAny() || !i.Index.GetBase().Type.IsAssignableTo(keyType) {
			err := fmt.Errorf("Map 键类型不匹配: 期望 %s, 实际 %s", keyType, i.Index.GetBase().Type)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if i.Object.GetBase().IsInvalid() && valType.IsAny() {
			err := errors.New(invalidReason(i.Object, "索引对象存在错误，无法精确推导索引结果"))
			ctx.AddErrorf("%s", err.Error())
			i.InvalidCause = err.Error()
			return err
		}

		if i.Multi {
			i.Type = CreateTupleType(valType, "Bool")
		} else {
			i.Type = valType
		}
		return nil
	}
	err := fmt.Errorf("索引访问的对象类型 %s 不支持", objType)
	ctx.AddErrorf("%s", err.Error())
	return err
}

func (i *IndexExpr) Optimize(ctx *OptimizeContext) Node {
	if i.Object != nil {
		if opt := i.Object.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				i.Object = val
			}
		}
	}
	if i.Index != nil {
		if opt := i.Index.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				i.Index = val
			}
		}
	}
	return i
}

// SliceExpr 表示切片表达式 (a[low:high])
type SliceExpr struct {
	BaseNode
	X    Expr
	Low  Expr // 可为 nil
	High Expr // 可为 nil
}

func (s *SliceExpr) GetBase() *BaseNode { return &s.BaseNode }
func (s *SliceExpr) exprNode()          {}

func (s *SliceExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(s)
	if s.X == nil {
		err := errors.New("slice 语句缺少对象")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if err := s.X.Check(ctx.WithNode(s.X)); err != nil {
		return err
	}
	xType := s.X.GetBase().Type
	if !xType.IsArray() && xType != "TypeBytes" && xType != "String" && !xType.IsAny() {
		err := fmt.Errorf("类型 %s 不支持切片操作", xType)
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if xType.IsAny() && s.X.GetBase().IsInvalid() {
		err := errors.New("切片对象存在前置错误，无法精确推导切片类型")
		ctx.AddErrorf("%s", err.Error())
		s.InvalidCause = err.Error()
		return err
	}
	if s.X.GetBase().IsInvalid() && xType.IsArray() {
		if elemType, ok := xType.ReadArrayItemType(); ok && elemType.IsAny() {
			err := errors.New(invalidReason(s.X, "切片对象存在错误，无法精确推导切片类型"))
			ctx.AddErrorf("%s", err.Error())
			s.InvalidCause = err.Error()
			return err
		}
	}

	if s.Low != nil {
		if err := s.Low.Check(ctx.WithNode(s.Low)); err != nil {
			return err
		}
		if s.Low.GetBase().IsInvalid() && s.Low.GetBase().Type.IsAny() {
			err := errors.New(invalidReason(s.Low, "slice low 索引存在前置错误，无法精确推导切片范围"))
			ctx.AddErrorf("%s", err.Error())
			s.InvalidCause = err.Error()
			return err
		}
		if !s.Low.GetBase().Type.IsNumeric() {
			err := errors.New("slice low 索引必须是数值类型")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	}
	if s.High != nil {
		if err := s.High.Check(ctx.WithNode(s.High)); err != nil {
			return err
		}
		if s.High.GetBase().IsInvalid() && s.High.GetBase().Type.IsAny() {
			err := errors.New(invalidReason(s.High, "slice high 索引存在前置错误，无法精确推导切片范围"))
			ctx.AddErrorf("%s", err.Error())
			s.InvalidCause = err.Error()
			return err
		}
		if !s.High.GetBase().Type.IsNumeric() {
			err := errors.New("slice high 索引必须是数值类型")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	}

	s.Type = xType // 切片结果类型保持一致
	return nil
}

func (s *SliceExpr) Optimize(ctx *OptimizeContext) Node {
	if s.X != nil {
		if opt := s.X.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				s.X = val
			}
		}
	}
	if s.Low != nil {
		if s.Low != nil {
			if opt := s.Low.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					s.Low = val
				}
			}
		}
	}
	if s.High != nil {
		if s.High != nil {
			if opt := s.High.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					s.High = val
				}
			}
		}
	}
	return s
}

// FuncLitExpr 表示匿名函数/闭包字面量表达式
type FuncLitExpr struct {
	BaseNode
	FunctionType `json:",inline"`
	Body         *BlockStmt `json:"body"`
	CaptureNames []string   `json:"capture_names,omitempty"` // 静态分析出的需要捕获的外部变量名
}

func (f *FuncLitExpr) exprNode() {}

func (f *FuncLitExpr) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(f)
	// 类型推导
	f.Type = TypeClosure

	// 这里我们需要在 SemanticContext 中开启一个新的作用域来检查函数体
	// 但这在现有的 check_*.go 或 ast_stmt.go 的 FunctionStmt.Check 中可能有现成的模式
	// 为了简单起见，我们委托给一个在 ast_valid.go 里的专用检查函数
	return checkFuncLit(f, ctx)
}

func (f *FuncLitExpr) Optimize(ctx *OptimizeContext) Node {
	if f.Body != nil {
		opt := f.Body.Optimize(ctx)
		if val, ok := opt.(*BlockStmt); ok {
			f.Body = val
		}
	}
	return f
}
