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
		ctx.AddErrorf("%s", err.Error())
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
	s.X = s.X.Optimize(ctx).(Expr)
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
	t.X = t.X.Optimize(ctx).(Expr)
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

	return fmt.Errorf("const/function %s 不存在", c.Name)
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

	funcErr := c.Func.Check(ctx)

	// 无论函数名是否能解析成功，我们都必须校验参数，以便填充 LSP 所需的类型信息
	var argsError bool

	// 特殊处理 make/new 的类型参数解析
	if ident, ok := c.Func.(*ConstRefExpr); ok && (ident.Name == "make" || ident.Name == "new") {
		if len(c.Args) > 0 {
			if lit, ok2 := c.Args[0].(*LiteralExpr); ok2 && lit.Type == "String" {
				t := GoMiniType(lit.Value).Resolve(ctx.ValidContext)
				if ident.Name == "make" {
					if !t.IsStrictValid() {
						ctx.AddErrorf("make: 非法类型 %s", lit.Value)
						argsError = true
					}
				} else { // new
					if !t.IsStrictValid() {
						if _, hasStruct := ctx.GetStruct(Ident(t)); !hasStruct {
							ctx.AddErrorf("new: 非法类型 %s", lit.Value)
							argsError = true
						}
					}
					t = t.ToPtr()
				}
				lit.Value = string(t)
			}
		}
	}

	for _, arg := range c.Args {
		if err := arg.Check(ctx); err != nil {
			argsError = true
		}
	}

	if funcErr != nil {
		return funcErr
	}

	fType, b := c.Func.GetBase().Type.ReadCallFunc()
	if !b {
		if c.Func.GetBase().Type.IsAny() {
			c.Type = "Any"
			if argsError {
				return errors.New("invalid arguments")
			}
			return nil
		}
		err := fmt.Errorf("对象(%s)不是函数", c.Func.GetBase().Type)
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	if argsError {
		return errors.New("invalid arguments")
	}

	// 语义校验：参数数量和基本类型匹配
	minParams := len(fType.Params)
	if fType.Variadic {
		minParams--
	}

	// 校验固定参数部分的类型
	fixedNum := len(fType.Params)
	isImplicitArray := len(fType.Params) > 0 && fType.Params[len(fType.Params)-1].IsArray()
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
	if !fType.Variadic && !isImplicitArray && len(fType.Params) > 0 && len(c.Args) > len(fType.Params) {
		return fmt.Errorf("函数参数数量过多: 需 %d, 实际 %d", len(fType.Params), len(c.Args))
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

	return nil
}

func (c *CallExprStmt) Optimize(ctx *OptimizeContext) Node {
	c.Func = c.Func.Optimize(ctx).(Expr)
	for i, arg := range c.Args {
		c.Args[i] = arg.Optimize(ctx).(Expr)
	}

	fType, ok := c.Func.GetBase().Type.ReadCallFunc()
	if ok && fType != nil {
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
	if objType == TypeModule || objType == "Any" {
		m.Type = "Any"
		return nil
	}

	if objType == "Error" {
		if m.Property == "Error" {
			m.Type = "function() String"
			return nil
		}
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
					m.Type = GoMiniType(method.String())
					return nil
				}
				// 继续尝试其他可能，不立即报错
			} else {
				m.Type = met
				return nil
			}
		} else if strings.Contains(typeName, ".") {
			// 如果是跨包的结构体（例如 lib.Point），由于在隔离架构下不静态合并符号表，
			// 在编译期无法得知其完整定义，因此放行作为动态成员访问
			m.Type = "Any"
			return nil
		}
	}

	return fmt.Errorf("type %s does not support member access to %s", objType, m.Property)
}

func (m *MemberExpr) Optimize(ctx *OptimizeContext) Node {
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
	if c.Kind != "" {
		c.Kind = Ident(GoMiniType(c.Kind).Resolve(ctx.ValidContext))
		c.Type = GoMiniType(c.Kind)
	}

	if c.Type == "" {
		// 尝试从 BaseNode 获取（可能由外层 Check 预设）
		c.Type = c.BaseNode.Type
	}

	if c.Type == "" || c.Type == "Any" {
		// 最后的启发式：根据是否有 Key 判定是 Array 还是 Map
		hasKey := false
		for _, v := range c.Values {
			if v.Key != nil {
				hasKey = true
				break
			}
		}
		if hasKey {
			c.Type = "Map<String, Any>"
		} else {
			c.Type = "Array<Any>"
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
	i.Object = i.Object.Optimize(ctx).(Expr)
	i.Index = i.Index.Optimize(ctx).(Expr)
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
	s.X = s.X.Optimize(ctx).(Expr)
	if s.Low != nil {
		s.Low = s.Low.Optimize(ctx).(Expr)
	}
	if s.High != nil {
		s.High = s.High.Optimize(ctx).(Expr)
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
		f.Body = f.Body.Optimize(ctx).(*BlockStmt)
	}
	return f
}
