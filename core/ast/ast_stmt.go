package ast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ImportSpec 表示包导入声明
type ImportSpec struct {
	Alias string `json:"alias,omitempty"` // 别名，默认为空表示使用包名
	Path  string `json:"path"`            // 导入路径
}

// ProgramStmt 程序启动
type ProgramStmt struct {
	BaseNode  `json:",inline"`
	Package   string                  `json:"package,omitempty"` // 包名，默认为main
	Imports   []ImportSpec            `json:"imports,omitempty"` // 导入列表
	Constants map[string]string       `json:"constants"`         // 常量表
	Variables map[Ident]Expr          `json:"variables"`         // 声明的全局变量
	Structs   map[Ident]*StructStmt   `json:"structs"`           // 声明的对象 (对象)
	Functions map[Ident]*FunctionStmt `json:"functions"`         // 声明的函数 (解构为无作用域函数)
	Main      []Stmt                  `json:"main"`              // 入口点 （如果没有内容则代表为 lib）
}

func (p *ProgramStmt) GetBase() *BaseNode {
	return &p.BaseNode
}
func (p *ProgramStmt) stmtNode() {}

func (p *ProgramStmt) Validate(ctx *ValidContext) (Node, bool) {
	if ctx.parent != nil {
		ctx.AddErrorf("程序入口必须为顶点")
		return nil, false
	}
	for i, structDef := range p.Structs {
		validate, b := structDef.Validate(ctx)
		if !b {
			return nil, false
		}
		p.Structs[i] = validate.(*StructStmt)
	}

	// 处理全局变量映射的转义
	newVars := make(map[Ident]Expr)
	for i, stmt := range p.Variables {
		mangledI := i
		if p.Package != "" && p.Package != "main" {
			if !strings.Contains(string(i), ".") {
				mangledI = Ident(fmt.Sprintf("%s.%s", p.Package, i))
			}
		}

		if !mangledI.Valid(ctx) {
			return nil, false
		}
		validate, b := stmt.Validate(ctx)
		if !b {
			return nil, false
		}
		newVars[mangledI] = validate.(Expr)
		ctx.root.Fields[mangledI] = validate.GetBase().Type
		ctx.AddVariable(mangledI, validate.GetBase().Type) // 关键：注册到 context
	}
	p.Variables = newVars

	for i, function := range p.Functions {
		validate, b := function.Validate(ctx)
		if !b {
			return nil, false
		}
		if validate != nil {
			p.Functions[i] = validate.(*FunctionStmt)
		}
	}
	var newMain []Stmt
	for _, node := range p.Main {
		validate, b := node.Validate(ctx)
		if !b {
			return nil, false
		}
		if validate != nil {
			if b, ok := validate.(*BlockStmt); ok {
				newMain = append(newMain, b.Children...)
			} else {
				newMain = append(newMain, validate.(Stmt))
			}
		}
	}
	p.Main = newMain
	return p, true
}

func (p *ProgramStmt) String() string {
	buffer := bytes.NewBuffer(nil)
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(p)
	return buffer.String()
}

// BlockStmt 表示代码块或作用域
type BlockStmt struct {
	BaseNode `json:",inline"`
	Children []Stmt `json:"children"`
	Inner    bool   `json:"inner,omitempty"` // 是否开启新作用域
}

func NewBlock(node Node, args ...Stmt) *BlockStmt {
	id := "block"
	var message string
	if node != nil {
		id = node.GetBase().ID
		message = node.GetBase().Message
	}
	if len(args) == 0 {
		args = []Stmt{}
	}
	return &BlockStmt{
		BaseNode: BaseNode{
			ID:      id,
			Meta:    "block",
			Type:    "Void",
			Message: message,
		},
		Children: args,
	}
}

func (b *BlockStmt) GetBase() *BaseNode { return &b.BaseNode }
func (b *BlockStmt) stmtNode()          {}

func (b *BlockStmt) Validate(ctx *ValidContext) (Node, bool) {
	if b.Children == nil {
		b.Children = make([]Stmt, 0)
	}

	blockScope := ctx
	if !b.Inner {
		blockScope = ctx.Child(b)
	}
	var newChildren []Stmt

	for _, child := range b.Children {
		childNode, ok := child.Validate(blockScope)
		if !ok {
			return nil, false
		}
		if childNode == nil {
			continue
		}
		// block 嵌套解除
		if block, ok := childNode.(*BlockStmt); ok {
			if len(block.Children) == 0 {
				continue
			}
			if block.Inner {
				newChildren = append(newChildren, block.Children...)
				continue
			}
		}
		newChildren = append(newChildren, childNode.(Stmt))
	}
	b.Children = newChildren
	b.Type = "Void"
	return b, true
}

// Param 表示函数参数定义
type Param struct {
	Name Ident `json:"name"`
	Type Ident `json:"type"`
}

// IfStmt 表示if条件语句
type IfStmt struct {
	BaseNode `json:",inline"`
	Cond     Expr       `json:"cond"`
	Body     *BlockStmt `json:"body"`
	ElseBody *BlockStmt `json:"else,omitempty"`
}

func (i *IfStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *IfStmt) stmtNode()          {}

func (i *IfStmt) Validate(ctx *ValidContext) (Node, bool) {
	ctx = ctx.Child(i)

	// 常量折叠优化
	if lit, ok := i.Cond.(*LiteralExpr); ok {
		if lit.Type == "Bool" {
			if lit.Value == "true" {
				// 总是执行 if 分支
				return i.Body.Validate(ctx)
			}
			// 总是执行 else 分支或空语句
			if i.ElseBody != nil {
				return i.ElseBody.Validate(ctx)
			}
			// 返回空 block
			return NewBlock(nil), true
		}
	}

	if i.Cond == nil {
		ctx.AddErrorf("if语句缺少条件表达式")
		return nil, false
	}

	condNode, ok := i.Cond.Validate(ctx)
	if !ok {
		return nil, false
	}
	i.Cond = condNode.(Expr)

	condType := i.Cond.GetBase().Type
	if condType == "" {
		ctx.Child(i.Cond).AddErrorf("if条件表达式类型无法推导")
		return nil, false
	}

	if !condType.Equals("Bool") {
		ctx.Child(i.Cond).AddErrorf("if表达式不是返回Bool类型, 实际为 %s", condType)
		return nil, false
	}

	if i.Body == nil {
		ctx.AddErrorf("if语句缺少主体")
		return nil, false
	}

	bodyNode, ok := i.Body.Validate(ctx)
	if !ok {
		return nil, false
	}
	_, isBlock := bodyNode.(*BlockStmt)
	if !isBlock {
		ctx.AddErrorf("if body 不是有效的语句块")
		return nil, false
	}
	i.Body = bodyNode.(*BlockStmt)
	if i.ElseBody != nil {
		elseNode, ok := i.ElseBody.Validate(ctx)
		if !ok {
			return nil, false
		}
		if elseNode != nil {
			el, isBlock := elseNode.(*BlockStmt)
			if !isBlock {
				ctx.AddErrorf("if else 不是有效的语句块")
				return nil, false
			}
			i.ElseBody = el
		} else {
			i.ElseBody = nil
		}
	}

	i.Type = "Void"
	return i, true
}

// ForStmt 表示for循环语句
type ForStmt struct {
	BaseNode `json:",inline"`
	Init     Node `json:"init,omitempty"` // force block or nil
	Cond     Expr `json:"cond,omitempty"`
	Update   Node `json:"update,omitempty"`
	Body     Node `json:"body"`
}

func (f *ForStmt) GetBase() *BaseNode { return &f.BaseNode }
func (f *ForStmt) stmtNode()          {}

func (f *ForStmt) Validate(ctx *ValidContext) (Node, bool) {
	ctx = ctx.Child(f)

	if f.Init != nil {
		if _, ok := f.Init.(*BlockStmt); !ok {
			initID := f.Init.GetBase().ID
			block := NewBlock(f.Init, f.Init.(Stmt))
			f.Init.GetBase().ID = initID + "_Children_0"
			block.Inner = true
			f.Init = block
		}
		initNode, ok := f.Init.Validate(ctx)
		if !ok {
			return nil, false
		}
		f.Init = initNode
	}

	if f.Cond != nil {
		condNode, ok := f.Cond.Validate(ctx)
		if !ok {
			return nil, false
		}
		f.Cond = condNode.(Expr)

		condType := f.Cond.GetBase().Type
		if condType != "" && !condType.Equals("Bool") {
			ctx.Child(f.Cond).AddErrorf("for循环条件必须是Bool类型, 实际为 %s", condType)
			return nil, false
		}
	}

	if f.Update != nil {
		if _, ok := f.Update.(*BlockStmt); !ok {
			updateID := f.Update.GetBase().ID
			block := NewBlock(f.Update, f.Update.(Stmt))
			f.Update.GetBase().ID = updateID + "_Children_0"
			block.Inner = true
			f.Update = block
		}
		updateNode, ok := f.Update.Validate(ctx)
		if !ok {
			return nil, false
		}
		f.Update = updateNode
	}
	if f.Body == nil {
		ctx.AddErrorf("for循环缺少主体")
		return nil, false
	}

	bodyNode, ok := f.Body.Validate(ctx)
	if !ok {
		return nil, false
	}
	f.Body = bodyNode

	if _, ok := f.Body.(*BlockStmt); !ok {
		ctx.AddErrorf("循环主体不是 block")
		return nil, false
	}

	f.Type = "Void"
	return f, true
}

// ReturnStmt 表示return返回语句
type ReturnStmt struct {
	BaseNode
	Results []Expr `json:"results"`
}

func (r *ReturnStmt) GetBase() *BaseNode { return &r.BaseNode }
func (r *ReturnStmt) stmtNode()          {}

func (r *ReturnStmt) Validate(ctx *ValidContext) (Node, bool) {
	if r.Results == nil {
		r.Results = make([]Expr, 0)
	}

	for i, result := range r.Results {
		resultNode, ok := result.Validate(ctx)
		if !ok {
			return nil, false
		}
		r.Results[i] = resultNode.(Expr)
	}

	scope, b := ctx.CheckScope(&FunctionStmt{})
	if !b {
		ctx.AddErrorf("return 语句只能在函数中使用")
		return nil, false
	}

	stmt := scope.(*FunctionStmt)
	if stmt.Return.IsVoid() && len(r.Results) != 0 {
		ctx.AddErrorf("当前函数不存在返回值")
		return nil, false
	}

	if len(r.Results) > 0 {
		var tType GoMiniType
		if len(r.Results) > 1 {
			var rTypes []GoMiniType
			for _, result := range r.Results {
				rTypes = append(rTypes, result.GetBase().Type)
			}
			tType = CreateTupleType(rTypes...)
		} else {
			tType = r.Results[0].GetBase().Type
		}

		if !tType.Equals(stmt.Return) {
			ctx.AddErrorf("返回类型错误 (return:%s != function:%s)", stmt.Return, tType)
			return nil, false
		}
	}

	r.Type = "Void"
	return r, true
}

// FunctionStmt 表示函数定义语句 todo: 作用域检查需确认
type FunctionStmt struct {
	BaseNode
	FunctionType `json:",inline"`
	Scope        Ident      `json:"scope,omitempty"` // 函数的作用域
	Name         Ident      `json:"name"`
	Body         *BlockStmt `json:"body"` // 函数结构体
}

func (f *FunctionStmt) GetBase() *BaseNode { return &f.BaseNode }
func (f *FunctionStmt) stmtNode()          {}

// Validate todo: 调整函数声明限制
func (f *FunctionStmt) Validate(ctx *ValidContext) (Node, bool) {
	funcCtx := ctx.Child(f)
	// 函数注册应该是全局注册
	funcCtx.parent = nil

	var structType *ValidStruct

	if ctx.root.Package != "" && ctx.root.Package != "main" {
		if !strings.Contains(string(f.Name), ".") {
			f.Name = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, f.Name))
		}
	}

	if f.Scope != "" {
		f.Scope = Ident(GoMiniType(f.Scope).Resolve(ctx))
		if !f.Scope.Valid(ctx) {
			return nil, false
		}

		var ok bool
		if structType, ok = ctx.GetStruct(f.Scope); !ok {
			funcCtx.AddErrorf("未知 struct %s", f.Scope)
			return nil, false
		}
	} else {
		structType = ctx.root.ValidStruct
	}

	if f.Name == "" {
		funcCtx.AddErrorf("函数定义缺少名称")
		return nil, false
	}

	if !f.Name.Valid(ctx) {
		return nil, false
	}

	if t, ok := structType.Methods[f.Name]; ok {
		funcCtx.AddErrorf("函数 %s 已被定义: %s", f.Name, t)
		return nil, false
	}

	// 验证函数类型
	f.FunctionType.Return = f.FunctionType.Return.Resolve(ctx)
	if !f.FunctionType.Return.Valid(ctx) {
		return nil, false
	}

	for i, param := range f.Params {
		f.Params[i].Type = param.Type.Resolve(ctx)
		if param.Name == "" || !param.Name.Valid(ctx) {
			return nil, false
		}
		if !f.Params[i].Type.Valid(ctx) {
			return nil, false
		}
		if f.Params[i].Type.IsVoid() {
			ctx.AddErrorf("%s 不接受 void 类型作为函数参数", param.Name)
			return nil, false
		}
	}

	// 创建函数作用域并添加参数
	bodyCtx := funcCtx.Child(f.Body)
	for _, param := range f.Params {
		if param.Name != "" {
			bodyCtx.AddVariable(param.Name, param.Type)
		}
	}

	// this 上下文
	if f.Scope != "" {
		bodyCtx.AddVariable("this", GoMiniType(f.Scope))
	}

	// 验证函数体
	bodyNode, ok := f.Body.Validate(bodyCtx)
	if !ok {
		return nil, false
	}
	if stmt, ok := bodyNode.(*BlockStmt); !ok {
		bodyID := stmt.GetBase().ID
		f.Body = NewBlock(stmt, stmt)
		f.Body.Inner = true
		stmt.GetBase().ID = bodyID + "_Children_0"
	} else {
		f.Body = stmt
		f.Body.Inner = true
	}

	// 如果不是void函数，进行返回路径分析
	returnTypes, _ := f.FunctionType.Return.ReadTuple()
	if len(returnTypes) > 0 && !(len(returnTypes) == 1 && returnTypes[0].IsVoid()) {
		analyzer := NewReturnAnalyzer(bodyCtx, f)
		if !analyzer.Analyze(f.Body) {
			analyzer.AddReturnPathErrorsToContext(funcCtx)
			return nil, false
		}
	}

	// 注册函数
	structType.Methods[f.Name] = f.FunctionType.ToCallFunctionType()
	if f.Scope != "" {
		err := ctx.AddFuncSpec(Ident(fmt.Sprintf("__obj__%s__%s", f.Scope, f.Name)), GoMiniType(f.FunctionType.String()))
		if err != nil {
			ctx.AddErrorf("添加全局函数失败")
			return nil, false
		}
	}
	f.Type = "Void"
	name := f.Name
	if f.Scope != "" {
		name = Ident(fmt.Sprintf("__obj__%s__%s", f.Scope, f.Name))
	}
	ctx.root.program.Functions[name] = &FunctionStmt{
		BaseNode: f.BaseNode,
		FunctionType: FunctionType{
			Params: f.Params,
			Return: f.Return,
		},
		Scope: "",
		Name:  name,
		Body:  f.Body,
	}
	return nil, true
}

// GenDeclStmt 变量声明
type GenDeclStmt struct {
	BaseNode
	Name Ident
	Kind GoMiniType
}

func (g *GenDeclStmt) GetBase() *BaseNode { return &g.BaseNode }
func (g *GenDeclStmt) stmtNode()          {}
func (g *GenDeclStmt) Validate(ctx *ValidContext) (Node, bool) {
	g.Type = "Void"
	g.Name = g.Name.Resolve(ctx)

	// 处理顶级变量命名空间
	if ctx.parent == nil && ctx.root.Package != "" && ctx.root.Package != "main" {
		if !strings.Contains(string(g.Name), ".") {
			g.Name = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, g.Name))
		}
	}

	if !g.Name.Valid(ctx) {
		return nil, false
	}
	g.Kind = g.Kind.Resolve(ctx)
	if !g.Kind.Valid(ctx) {
		return nil, false
	}
	if _, b := ctx.GetVariable(g.Name); b {
		ctx.AddErrorf("variable %s already exists", g.Name)
		return nil, false
	}
	ctx.AddVariable(g.Name, g.Kind)
	return g, true
}

// AssignmentStmt 表示赋值语句
type AssignmentStmt struct {
	BaseNode
	Variable Ident `json:"variable"`
	Property Ident `json:"property,omitempty"` // 新增：支持成员赋值 (a.b = x)
	Value    Expr  `json:"value"`
}

func (a *AssignmentStmt) GetBase() *BaseNode { return &a.BaseNode }
func (a *AssignmentStmt) stmtNode()          {}

// DeferStmt 表示延迟执行语句
type DeferStmt struct {
	BaseNode
	Call Expr `json:"call"`
}

func (d *DeferStmt) GetBase() *BaseNode { return &d.BaseNode }
func (d *DeferStmt) stmtNode()          {}

func (d *DeferStmt) Validate(ctx *ValidContext) (Node, bool) {
	d.Type = "Void"
	if d.Call == nil {
		ctx.AddErrorf("defer 语句缺少调用表达式")
		return nil, false
	}
	callNode, ok := d.Call.Validate(ctx)
	if !ok {
		return nil, false
	}
	d.Call = callNode.(Expr)
	return d, true
}

func (a *AssignmentStmt) Validate(ctx *ValidContext) (Node, bool) {
	a.Type = "Void"
	a.Variable = a.Variable.Resolve(ctx)

	// 1. 查找变量（GetVariable 会处理本包 Mangling 回退）
	vType, b := ctx.GetVariable(a.Variable)

	// 如果直接找没找到，且没带点，尝试加包名前缀找（针对本包全局变量引用）
	if !b && !strings.Contains(string(a.Variable), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, a.Variable))
		if vt, ok := ctx.GetVariable(mangled); ok {
			a.Variable = mangled
			vType = vt
			b = true
		}
	}

	if !a.Variable.Valid(ctx) {
		return nil, false
	}
	if a.Value == nil {
		ctx.AddErrorf("赋值语句缺少值")
		return nil, false
	}
	valueNode, ok := a.Value.Validate(ctx)
	if !ok {
		return nil, false
	}
	a.Value = valueNode.(Expr)
	miniType := a.Value.GetBase().Type
	if miniType.IsEmpty() {
		ctx.AddErrorf("无法推导类型")
		return nil, false
	}
	if miniType.IsVoid() {
		ctx.AddErrorf("类型 (%s) 不支持赋值", miniType)
		return nil, false
	}

	if a.Property != "" {
		if !b {
			ctx.AddErrorf("变量 %s 不存在", a.Variable)
			return nil, false
		}
		struName := vType
		if vType.IsPtr() {
			elem, _ := vType.GetPtrElementType()
			struName = elem
		}
		miniStruct, b2 := ctx.GetStruct(Ident(struName))
		if !b2 {
			ctx.AddErrorf("类型 %s 未定义", struName)
			return nil, false
		}
		fieldType, ok2 := miniStruct.Fields[a.Property]
		if !ok2 {
			ctx.AddErrorf("结构体 %s 不存在字段 %s", struName, a.Property)
			return nil, false
		}
		if !fieldType.Equals(miniType) {
			if ptr, ok3 := fieldType.AutoPtr(a.Value); ok3 {
				a.Value = ptr
			} else {
				ctx.AddErrorf("字段赋值类型不一致: 需 %s, 实际 %s", fieldType, miniType)
				return nil, false
			}
		}
		return a, true
	}

	if b {
		if !vType.Equals(miniType) {
			if ptr, ok3 := vType.AutoPtr(a.Value); ok3 {
				a.Value = ptr
			} else {
				ctx.AddErrorf("对象类型不一致 (%s != %s)，无法赋值", vType, miniType)
				return nil, false
			}
		}
		return a, true
	}

	// 如果是顶级且未定义，触发声明并转义
	if ctx.parent == nil && ctx.root.Package != "" && ctx.root.Package != "main" {
		if !strings.Contains(string(a.Variable), ".") {
			a.Variable = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, a.Variable))
		}
	}

	genID := a.ID + "_Children_0"
	aID := a.ID
	a.ID += "_Children_1"
	block := NewBlock(nil, &GenDeclStmt{
		BaseNode: BaseNode{
			ID:      genID,
			Meta:    "generate",
			Type:    "Void",
			Message: a.Message,
		},
		Name: a.Variable,
		Kind: miniType,
	}, a)
	block.ID = aID
	block.Inner = true
	return block.Validate(ctx)
}

// InterruptStmt 表示中断语句（break/continue）
type InterruptStmt struct {
	BaseNode
	InterruptType string `json:"interrupt_type"` // "break" 或 "continue"
}

func (i *InterruptStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *InterruptStmt) stmtNode()          {}

func (i *InterruptStmt) Validate(ctx *ValidContext) (Node, bool) {
	ctx = ctx.Child(i)

	if i.InterruptType != "break" && i.InterruptType != "continue" {
		ctx.AddErrorf("无效的中断类型: %s", i.InterruptType)
		return nil, false
	}

	if _, ok := ctx.CheckScope(&ForStmt{}); !ok {
		ctx.AddErrorf("%s 语句只能在循环中使用", i.InterruptType)
		return nil, false
	}

	i.Type = "Void"
	return i, true
}

// DerefAssignmentStmt 表示解引用赋值语句 *p = v
type DerefAssignmentStmt struct {
	BaseNode
	Object Expr `json:"object"` // 指针对象表达式
	Value  Expr `json:"value"`  // 新值表达式
}

func (d *DerefAssignmentStmt) GetBase() *BaseNode { return &d.BaseNode }
func (d *DerefAssignmentStmt) stmtNode()          {}

func (d *DerefAssignmentStmt) Validate(ctx *ValidContext) (Node, bool) {
	d.Type = "Void"
	if d.Object == nil {
		ctx.AddErrorf("解引用赋值缺少对象")
		return nil, false
	}
	objNode, ok := d.Object.Validate(ctx)
	if !ok {
		return nil, false
	}
	d.Object = objNode.(Expr)

	if d.Value == nil {
		ctx.AddErrorf("解引用赋值缺少值")
		return nil, false
	}
	valNode, ok := d.Value.Validate(ctx)
	if !ok {
		return nil, false
	}
	d.Value = valNode.(Expr)

	// 检查对象是否为指针类型
	objType := d.Object.GetBase().Type
	if !objType.IsPtr() {
		ctx.Child(d.Object).AddErrorf("解引用赋值的对象必须是指针类型，实际为 %s", objType)
		return nil, false
	}

	// 检查类型是否匹配
	elemType, _ := objType.GetPtrElementType()
	valType := d.Value.GetBase().Type
	if !elemType.Equals(valType) {
		// 尝试自动指针转换
		if ptr, ok := elemType.AutoPtr(d.Value); ok {
			d.Value = ptr
		} else {
			ctx.AddErrorf("解引用赋值类型不一致: 需 %s, 实际 %s", elemType, valType)
			return nil, false
		}
	}

	return d, true
}

// StructStmt 所有 struct 都注册到全局
type StructStmt struct {
	BaseNode
	Name   Ident                `json:"name"`
	Fields map[Ident]GoMiniType `json:"fields"`
}

func (s *StructStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *StructStmt) stmtNode()          {}

func (s *StructStmt) Validate(parentCtx *ValidContext) (Node, bool) {
	ctx := parentCtx.Child(s)
	if s.Name == "" {
		ctx.AddErrorf("struct定义缺少名称")
		return nil, false
	}

	if parentCtx.root.Package != "" && parentCtx.root.Package != "main" {
		if !strings.Contains(string(s.Name), ".") {
			s.Name = Ident(fmt.Sprintf("%s.%s", parentCtx.root.Package, s.Name))
		}
	}

	if !s.Name.Valid(ctx) {
		return nil, false
	}

	// 验证字段
	for fieldName, fieldType := range s.Fields {
		s.Fields[fieldName] = fieldType.Resolve(ctx)
		if !fieldName.Valid(ctx) {
			return nil, false
		}
		if !s.Fields[fieldName].Valid(ctx) {
			return nil, false
		}
	}
	_ = parentCtx.AddStructDefine("Constant", nil)
	// 注册struct到上下文
	if err := parentCtx.AddStructDefine(s.Name, s.Fields); err != nil {
		ctx.AddErrorf("定义struct失败: %v", err)
		return nil, false
	}

	s.Type = "Void"
	parentCtx.root.program.Structs[s.Name] = s
	return nil, true
}
