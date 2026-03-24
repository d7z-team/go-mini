package ast

import (
	"errors"
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
	if s.X == nil {
		return errors.New("解引用缺少对象")
	}
	if err := s.X.Check(ctx); err != nil {
		return err
	}
	xType := s.X.GetBase().Type
	if xType.IsPtr() {
		if elem, ok := xType.GetPtrElementType(); ok {
			s.Type = elem
		} else {
			s.Type = "Any"
		}
	} else if xType == "TypeHandle" || xType.IsAny() {
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
	if t.X == nil {
		return errors.New("类型断言缺少对象")
	}
	if err := t.X.Check(ctx); err != nil {
		return err
	}
	if !t.Type.Valid(ctx.ValidContext) {
		return fmt.Errorf("无效的断言类型: %s", t.Type)
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
		_ = arg.Check(ctx.WithNode(arg))
	}

	if funcErr != nil {
		return funcErr
	}

	fType, b := c.Func.GetBase().Type.ReadCallFunc()
	if !b {
		if c.Func.GetBase().Type.IsAny() {
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
			return errors.New("invalid use of ellipsis with no arguments")
		}
		// 校验最后一个参数必须是数组类型
		lastArgType := c.Args[len(c.Args)-1].GetBase().Type
		if !lastArgType.IsArray() && !lastArgType.IsAny() {
			return fmt.Errorf("invalid use of ellipsis with non-array type %s", lastArgType)
		}
		goto done // 变长参数展开跳过常规数量检查，运行时处理
	}

	if len(c.Args) < minParams {
		return fmt.Errorf("函数参数数量不足: 需至少 %d, 实际 %d", minParams, len(c.Args))
	}
	if !fType.Variadic && !isImplicitArray && len(sigParams) > 0 && len(c.Args) > len(sigParams) {
		return fmt.Errorf("函数参数数量过多: 需 %d, 实际 %d", len(sigParams), len(c.Args))
	}

	for i := 0; i < fixedNum && i < len(c.Args); i++ {
		argType := c.Args[i].GetBase().Type
		if !sigParams[i].Equals(argType) {
			if _, ok := sigParams[i].AutoPtr(c.Args[i]); !ok {
				return fmt.Errorf("函数第 %d 个参数类型不匹配: 期望 %s, 实际 %s", i+1, sigParams[i], argType)
			}
		}
	}

	// 校验隐式数组/变长参数的子项兼容性
	if isImplicitArray && len(c.Args) > fixedNum {
		targetArrayType := sigParams[len(sigParams)-1]
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
		case "len":
			c.Type = "Int64"
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
					arg := tryAutoNumericCast(ctx.ValidContext, param, c.Args[i])
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

	if err := m.Object.Check(ctx); err != nil {
		return err
	}

	objType := m.Object.GetBase().Type
	if objType == "Any" {
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
			path, isPkg := ctx.root.Imports[string(id.Name)]
			if !isPkg {
				path = string(id.Name)
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
					m.Type = t
					return nil
				}
				// 2. 尝试作为函数查找
				if fn, ok := ctx.GetFunction(fullPath); ok {
					m.Type = fn.MiniType()
					return nil
				}
				// 3. 尝试作为结构体查找
				if _, ok := ctx.GetStruct(fullPath); ok {
					m.Type = GoMiniType(fullPath)
					return nil
				}
				// 4. 尝试作为接口查找
				if _, ok := ctx.GetInterface(fullPath); ok {
					m.Type = GoMiniType(fullPath)
					return nil
				}
			}

			err := fmt.Errorf("包 %s 不存在成员 %s", id.Name, m.Property)
			ctx.WithNode(m).AddErrorf("%s", err.Error())
			return err
		}
		m.Type = "Any"
		return nil
	}

	if objType.IsInterface() {
		methods, _ := objType.ReadInterfaceMethods()
		if sig, ok := methods[string(m.Property)]; ok {
			m.Type = sig.MiniType() // 使用解析出的完整签名类型
			return nil
		}
		return fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
	}

	// 检查是否为命名接口
	if iStmt, ok := ctx.GetInterface(Ident(objType)); ok {
		methods, _ := iStmt.Type.ReadInterfaceMethods()
		if sig, ok := methods[string(m.Property)]; ok {
			m.Type = sig.MiniType()
			return nil
		}
		return fmt.Errorf("interface %s does not support member access to %s", objType, m.Property)
	}

	if objType.IsMap() {
		_, vType, ok := objType.GetMapKeyValueTypes()
		if ok {
			m.Type = vType
			return nil
		}
	}

	if objType.IsAny() {
		m.Type = "Any"
		return nil
	}

	if !m.Property.Valid(ctx.ValidContext) {
		return fmt.Errorf("invalid property: %s", m.Property)
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
						if len(sig.Params) > 0 {
							sig.Params = sig.Params[1:]
						}
					}
					m.Type = sig.MiniType()
					return nil
				}
				// 找到了结构体但没找到字段和方法，跳过，最终会报错
			} else {
				m.Type = met
				return nil
			}
		} else {
			// 结构体未在当前上下文中定义（例如 FFI 宿主类型或跨包类型）。
			// 由于隔离架构不要求全量 AST，这里放行作为动态成员访问。
			m.Type = "Any"
			return nil
		}
	}

	return fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
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
					kt := v.Key.GetBase().Type
					if i == 0 {
						commonKeyType = kt
					} else if !kt.Equals(commonKeyType) {
						allSameKey = false
					}
				}
			} else {
				allSameKey = false
			}

			if v.Value != nil {
				if err := v.Value.Check(ctx); err == nil {
					vt := v.Value.GetBase().Type
					if i == 0 {
						commonValType = vt
					} else if !vt.Equals(commonValType) {
						allSameVal = false
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

	for _, elem := range c.Values {
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
	Multi  bool `json:"multi,omitempty"` // 为 true 时返回 (val, ok) Tuple
}

func (i *IndexExpr) GetBase() *BaseNode { return &i.BaseNode }
func (i *IndexExpr) exprNode()          {}

func (i *IndexExpr) Check(ctx *SemanticContext) error {
	if i.Object == nil {
		return errors.New("索引访问缺少对象表达式")
	}

	if err := i.Object.Check(ctx); err != nil {
		return err
	}

	if i.Index == nil {
		return errors.New("索引访问缺少索引表达式")
	}

	if err := i.Index.Check(ctx); err != nil {
		return err
	}

	objType := i.Object.GetBase().Type

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
			return errors.New("数组索引不支持二元解构语法")
		}
		// fmt.Printf("DEBUG: Index type: %s\n", i.Index.GetBase().Type)
		if i.Index.GetBase().Type != "Int64" && !i.Index.GetBase().Type.IsAny() {
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
			err := fmt.Errorf("无法获取Map类型信息: %s", objType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if !keyType.Equals(i.Index.GetBase().Type) {
			err := fmt.Errorf("Map索引类型不匹配: 需 %s, 实际 %s", keyType, i.Index.GetBase().Type)
			ctx.WithNode(i.Index).AddErrorf("%s", err.Error())
			return err
		}
		if i.Multi {
			i.Type = CreateTupleType(valType, "Bool")
		} else {
			i.Type = valType
		}
		return nil
	}
	return fmt.Errorf("索引访问的对象必须是数组或Map类型，实际为 %s", objType)
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
	if s.X == nil {
		return errors.New("slice 语句缺少对象")
	}
	if err := s.X.Check(ctx); err != nil {
		return err
	}
	xType := s.X.GetBase().Type
	if !xType.IsArray() && xType != "TypeBytes" && !xType.IsAny() {
		return fmt.Errorf("类型 %s 不支持切片操作", xType)
	}

	if s.Low != nil {
		if err := s.Low.Check(ctx); err != nil {
			return err
		}
		if !s.Low.GetBase().Type.IsNumeric() {
			return errors.New("slice low 索引必须是数值类型")
		}
	}
	if s.High != nil {
		if err := s.High.Check(ctx); err != nil {
			return err
		}
		if !s.High.GetBase().Type.IsNumeric() {
			return errors.New("slice high 索引必须是数值类型")
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
