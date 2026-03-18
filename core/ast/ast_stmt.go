package ast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

func (p *ProgramStmt) Check(ctx *SemanticContext) error {
	// 处理模块加载
	if p.Imports != nil {
		for _, imp := range p.Imports {
			if err := ctx.ImportPackage(imp.Path); err != nil {
				return err
			}
		}
	}

	// 预注册所有导入的包别名，以支持 MemberExpr/StructCallExpr 的静态查找
	for alias := range ctx.root.Imports {
		ctx.AddVariable(Ident(alias), "Package")
	}

	// 第一遍：预注册所有结构体
	for _, structDef := range p.Structs {
		structDef.GetBase().EnsureID(&ctx.ValidContext)
		if !structDef.PreRegister(&ctx.ValidContext) {
			return fmt.Errorf("struct %s pre-registration failed", structDef.Name)
		}
	}
	for _, stmt := range p.Main {
		if s, ok := stmt.(*StructStmt); ok {
			s.GetBase().EnsureID(&ctx.ValidContext)
			if !s.PreRegister(&ctx.ValidContext) {
				return fmt.Errorf("struct %s pre-registration failed", s.Name)
			}
		}
	}

	// 第二遍：预注册所有函数签名
	for name, function := range p.Functions {
		function.GetBase().EnsureID(&ctx.ValidContext)
		if _, ok := function.PreRegister(&ctx.ValidContext); !ok {
			return fmt.Errorf("function %s pre-registration failed", name)
		}
	}
	for _, stmt := range p.Main {
		if f, ok := stmt.(*FunctionStmt); ok {
			f.GetBase().EnsureID(&ctx.ValidContext)
			if _, ok := f.PreRegister(&ctx.ValidContext); !ok {
				return fmt.Errorf("function %s pre-registration failed", f.Name)
			}
		}
	}

	// 第三遍：全量语义校验（Check）
	// 注意：这里必须遍历所有路径，不进行任何剪枝优化
	for _, structDef := range p.Structs {
		if err := structDef.Check(ctx); err != nil {
			return err
		}
	}

	for i, stmt := range p.Variables {
		if !i.Valid(&ctx.ValidContext) {
			return fmt.Errorf("invalid identifier: %s", i)
		}
		if err := stmt.Check(ctx); err != nil {
			return err
		}
		// 注册到符号表
		ctx.root.Global.Fields[i] = stmt.GetBase().Type
		ctx.AddVariable(i, stmt.GetBase().Type)
	}

	for _, function := range p.Functions {
		if err := function.Check(ctx); err != nil {
			return err
		}
	}

	for _, node := range p.Main {
		if err := node.Check(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (p *ProgramStmt) Optimize(ctx *OptimizeContext) Node {
	// 1. 优化结构体定义
	for i, structDef := range p.Structs {
		p.Structs[i] = structDef.Optimize(ctx).(*StructStmt)
	}

	// 2. 优化全局变量定义
	newVars := make(map[Ident]Expr)
	for i, stmt := range p.Variables {
		newVars[i] = stmt.Optimize(ctx).(Expr)
	}
	p.Variables = newVars

	// 3. 优化函数定义
	for i, function := range p.Functions {
		optimized := function.Optimize(ctx)
		if optimized != nil {
			p.Functions[i] = optimized.(*FunctionStmt)
		}
	}

	// 4. 处理 Main 块中的语句，并执行定义提取
	var newMain []Stmt
	for _, node := range p.Main {
		optimized := node.Optimize(ctx)
		if optimized == nil {
			continue
		}

		// 如果是 FunctionStmt 或 StructStmt，将其移至全局表并从 Main 中移除
		if fn, ok := optimized.(*FunctionStmt); ok {
			p.Functions[fn.Name] = fn
			continue
		}
		if st, ok := optimized.(*StructStmt); ok {
			p.Structs[st.Name] = st
			continue
		}

		// 处理顶级变量声明提升：如果是被 Mangle 过的 AssignmentStmt，且 Variables 中尚不存在
		if assign, ok := optimized.(*AssignmentStmt); ok {
			if ident, ok := assign.LHS.(*IdentifierExpr); ok && strings.Contains(string(ident.Name), ".") {
				if _, exists := p.Variables[ident.Name]; !exists {
					p.Variables[ident.Name] = assign.Value
					// 为了兼容 E2E 测试中的 Mangle 校验，我们将其转换为 GenDeclStmt 形式放回 Main
					genDecl := &GenDeclStmt{
						BaseNode: assign.BaseNode,
						Name:     ident.Name,
						Kind:     assign.Value.GetBase().Type,
					}
					genDecl.Meta = "gen_decl"
					newMain = append(newMain, genDecl)
					// 同时保留 AssignmentStmt 的副作用（执行初始化）
					newMain = append(newMain, assign)
					continue
				}
			}
		}

		if stmt, ok := optimized.(Stmt); ok {
			newMain = append(newMain, stmt)
		}
	}
	p.Main = newMain
	return p
}

func (f *FunctionStmt) MiniType() GoMiniType {
	return f.FunctionType.MiniType()
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

func (b *BlockStmt) Check(ctx *SemanticContext) error {
	if b.Children == nil {
		b.Children = make([]Stmt, 0)
	}

	blockScope := &ctx.ValidContext
	if !b.Inner {
		blockScope = ctx.Child(b)
	}
	semCtx := NewSemanticContext(blockScope)

	for _, child := range b.Children {
		if err := child.Check(semCtx); err != nil {
			return err
		}
	}
	return nil
}

func (b *BlockStmt) Optimize(ctx *OptimizeContext) Node {
	if b.Children == nil {
		b.Children = make([]Stmt, 0)
	}

	var newChildren []Stmt
	for _, child := range b.Children {
		optimized := child.Optimize(ctx)
		if optimized == nil {
			continue
		}

		// 移除定义语句并确保已注册
		if fn, ok := optimized.(*FunctionStmt); ok {
			ctx.root.program.Functions[fn.Name] = fn
			continue
		}
		if st, ok := optimized.(*StructStmt); ok {
			ctx.root.program.Structs[st.Name] = st
			continue
		}

		// block 嵌套解除
		if block, ok := optimized.(*BlockStmt); ok {
			if len(block.Children) == 0 {
				continue
			}
			if block.Inner {
				newChildren = append(newChildren, block.Children...)
				continue
			}
		}
		newChildren = append(newChildren, optimized.(Stmt))
	}
	b.Children = newChildren
	b.Type = "Void"
	return b
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

func (i *IfStmt) Check(ctx *SemanticContext) error {
	semCtx := NewSemanticContext(ctx.Child(i))

	// 1. 检查 Cond 是否为空，Check Cond。
	if i.Cond == nil {
		return errors.New("if语句缺少条件表达式")
	}
	if err := i.Cond.Check(semCtx); err != nil {
		return err
	}

	// 2. 检查 Cond 类型是否为 Bool，无法推导或非 Bool 则报错。
	condType := i.Cond.GetBase().Type
	if condType == "" {
		return errors.New("if条件表达式类型无法推导")
	}
	if !condType.Equals("Bool") {
		return fmt.Errorf("if表达式不是返回Bool类型, 实际为 %s", condType)
	}

	// 3. 检查 Body 是否为空，Check Body。
	if i.Body == nil {
		return errors.New("if语句缺少主体")
	}
	if err := i.Body.Check(semCtx); err != nil {
		return err
	}

	// 4. 如果有 ElseBody，Check ElseBody。
	if i.ElseBody != nil {
		if err := i.ElseBody.Check(semCtx); err != nil {
			return err
		}
	}

	// 5. 必须全量遍历所有分支。
	return nil
}

func (i *IfStmt) Optimize(ctx *OptimizeContext) Node {
	// 1. Optimize Cond。
	i.Cond = i.Cond.Optimize(ctx).(Expr)

	// 2. 如果 Cond 是 LiteralExpr 且 Value 为 "true"，直接返回 Optimize 后的 Body。
	if lit, ok := i.Cond.(*LiteralExpr); ok && lit.Type == "Bool" && lit.Value == "true" {
		return i.Body.Optimize(ctx)
	}

	// 3. 如果 Cond 是 LiteralExpr 且 Value 为 "false"，直接返回 Optimize 后的 ElseBody（如果没有 ElseBody 则返回 nil）。
	if lit, ok := i.Cond.(*LiteralExpr); ok && lit.Type == "Bool" && lit.Value == "false" {
		if i.ElseBody != nil {
			return i.ElseBody.Optimize(ctx)
		}
		return nil
	}

	// 4. 递归 Optimize Body 和 ElseBody。
	i.Body = i.Body.Optimize(ctx).(*BlockStmt)
	if i.ElseBody != nil {
		optimizedElse := i.ElseBody.Optimize(ctx)
		if optimizedElse != nil {
			i.ElseBody = optimizedElse.(*BlockStmt)
		} else {
			i.ElseBody = nil
		}
	}

	i.Type = "Void"
	return i
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

func (f *ForStmt) Check(ctx *SemanticContext) error {
	semCtx := NewSemanticContext(ctx.Child(f))

	if f.Init != nil {
		if err := f.Init.Check(semCtx); err != nil {
			return err
		}
	}

	if f.Cond != nil {
		if err := f.Cond.Check(semCtx); err != nil {
			return err
		}
		condType := f.Cond.GetBase().Type
		if condType != "" && !condType.Equals("Bool") {
			return fmt.Errorf("for循环条件必须是Bool类型, 实际为 %s", condType)
		}
	}

	if f.Update != nil {
		if err := f.Update.Check(semCtx); err != nil {
			return err
		}
	}

	if f.Body == nil {
		return errors.New("for循环缺少主体")
	}

	if err := f.Body.Check(semCtx); err != nil {
		return err
	}

	if _, ok := f.Body.(*BlockStmt); !ok {
		return errors.New("循环主体不是 block")
	}

	f.Type = "Void"
	return nil
}

func (f *ForStmt) Optimize(ctx *OptimizeContext) Node {
	if f.Init != nil {
		f.Init = f.Init.Optimize(ctx).(Stmt)
	}
	if f.Cond != nil {
		f.Cond = f.Cond.Optimize(ctx).(Expr)
	}
	if f.Update != nil {
		f.Update = f.Update.Optimize(ctx).(Stmt)
	}
	f.Body = f.Body.Optimize(ctx).(Stmt)
	return f
}

// ReturnStmt 表示return返回语句
type ReturnStmt struct {
	BaseNode
	Results []Expr `json:"results"`
}

func (r *ReturnStmt) GetBase() *BaseNode { return &r.BaseNode }
func (r *ReturnStmt) stmtNode()          {}

func (r *ReturnStmt) Check(ctx *SemanticContext) error {
	if r.Results == nil {
		r.Results = make([]Expr, 0)
	}

	for _, result := range r.Results {
		if err := result.Check(ctx); err != nil {
			return err
		}
	}

	scope, b := ctx.CheckAnyScope("function", "func_lit")
	if !b {
		return errors.New("return 语句只能在函数中使用")
	}

	var expectedReturn GoMiniType
	if stmt, ok := scope.(*FunctionStmt); ok {
		expectedReturn = stmt.Return
	} else if expr, ok := scope.(*FuncLitExpr); ok {
		expectedReturn = expr.Return
	} else {
		return errors.New("未知的函数范围类型")
	}

	if expectedReturn.IsVoid() && len(r.Results) != 0 {
		return errors.New("当前函数不存在返回值")
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

		if !tType.Equals(expectedReturn) {
			return fmt.Errorf("返回类型错误 (return:%s != function:%s)", expectedReturn, tType)
		}
	}

	r.Type = "Void"
	return nil
}

func (r *ReturnStmt) Optimize(ctx *OptimizeContext) Node {
	for i, result := range r.Results {
		r.Results[i] = result.Optimize(ctx).(Expr)
	}
	return r
}

// DeferStmt 表示延迟执行语句
type DeferStmt struct {
	BaseNode
	Call Expr
}

func (d *DeferStmt) GetBase() *BaseNode { return &d.BaseNode }
func (d *DeferStmt) stmtNode()          {}

func (d *DeferStmt) Check(ctx *SemanticContext) error {
	d.Type = "Void"
	if d.Call == nil {
		return errors.New("defer 语句缺少调用表达式")
	}
	return d.Call.Check(ctx)
}

func (d *DeferStmt) Optimize(ctx *OptimizeContext) Node {
	d.Call = d.Call.Optimize(ctx).(Expr)
	return d
}

// RangeStmt 表示 range 遍历语句
type RangeStmt struct {
	BaseNode
	Key    Ident // 可选
	Value  Ident // 可选
	X      Expr
	Body   *BlockStmt
	Define bool // 是否是 := 形式
}

func (r *RangeStmt) GetBase() *BaseNode { return &r.BaseNode }
func (r *RangeStmt) stmtNode()          {}

func (r *RangeStmt) Check(ctx *SemanticContext) error {
	r.Type = "Void"
	if r.X == nil {
		return errors.New("range 语句缺少对象")
	}
	if err := r.X.Check(ctx); err != nil {
		return err
	}
	objType := r.X.GetBase().Type
	if !objType.IsArray() && !objType.IsMap() && !objType.IsAny() {
		return fmt.Errorf("range 语句不支持类型 %s", objType)
	}

	// 注册循环变量
	inner := NewSemanticContext(ctx.Child(r.Body))
	if r.Key != "" {
		kType := GoMiniType("Int64")
		if objType.IsMap() {
			kType, _, _ = objType.GetMapKeyValueTypes()
		}
		inner.AddVariable(r.Key, kType)
	}
	if r.Value != "" {
		vType := GoMiniType("Any")
		if objType.IsArray() {
			vType, _ = objType.ReadArrayItemType()
		} else if objType.IsMap() {
			_, vType, _ = objType.GetMapKeyValueTypes()
		}
		inner.AddVariable(r.Value, vType)
	}

	return r.Body.Check(inner)
}

func (r *RangeStmt) Optimize(ctx *OptimizeContext) Node {
	r.X = r.X.Optimize(ctx).(Expr)
	r.Body = r.Body.Optimize(ctx).(*BlockStmt)
	return r
}

// SwitchStmt 表示 switch 分支语句
type SwitchStmt struct {
	BaseNode
	Init Stmt
	Tag  Expr
	Body *BlockStmt // 包含多个 CaseClause
}

func (s *SwitchStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *SwitchStmt) stmtNode()          {}

func (s *SwitchStmt) Check(ctx *SemanticContext) error {
	s.Type = "Void"
	inner := NewSemanticContext(ctx.Child(s.Body))
	if s.Init != nil {
		if err := s.Init.Check(inner); err != nil {
			return err
		}
	}
	tagType := GoMiniType("Bool")
	if s.Tag != nil {
		if err := s.Tag.Check(inner); err != nil {
			return err
		}
		tagType = s.Tag.GetBase().Type
	}

	for _, child := range s.Body.Children {
		clause, ok := child.(*CaseClause)
		if !ok {
			return fmt.Errorf("switch 语句只能包含 CaseClause, 得到 %T", child)
		}
		if err := clause.CheckWithTag(inner, tagType); err != nil {
			return err
		}
	}
	return nil
}

func (s *SwitchStmt) Optimize(ctx *OptimizeContext) Node {
	if s.Init != nil {
		s.Init = s.Init.Optimize(ctx).(Stmt)
	}
	if s.Tag != nil {
		s.Tag = s.Tag.Optimize(ctx).(Expr)
	}
	s.Body = s.Body.Optimize(ctx).(*BlockStmt)
	return s
}

// CaseClause 表示 switch 中的 case 分支
type CaseClause struct {
	BaseNode
	List []Expr // nil 表示 default
	Body []Stmt
}

func (c *CaseClause) GetBase() *BaseNode { return &c.BaseNode }
func (c *CaseClause) stmtNode()          {}

func (c *CaseClause) Check(ctx *SemanticContext) error {
	return errors.New("CaseClause should be checked via SwitchStmt")
}

func (c *CaseClause) CheckWithTag(ctx *SemanticContext, tagType GoMiniType) error {
	c.Type = "Void"
	for i, expr := range c.List {
		if err := expr.Check(ctx); err != nil {
			return err
		}
		if !expr.GetBase().Type.IsAssignableTo(tagType) {
			return fmt.Errorf("case 类型不匹配: 期望 %s, 实际 %s", tagType, expr.GetBase().Type)
		}
		c.List[i] = expr.Optimize(NewOptimizeContext(&ctx.ValidContext)).(Expr)
	}
	for i, stmt := range c.Body {
		if err := stmt.Check(ctx); err != nil {
			return err
		}
		c.Body[i] = stmt.Optimize(NewOptimizeContext(&ctx.ValidContext)).(Stmt)
	}
	return nil
}

func (c *CaseClause) Optimize(ctx *OptimizeContext) Node {
	return c
}

// FunctionStmt 表示函数定义语句
type FunctionStmt struct {
	BaseNode
	FunctionType `json:",inline"`
	Scope        Ident      `json:"scope,omitempty"` // 函数的作用域
	Name         Ident      `json:"name"`
	Body         *BlockStmt `json:"body"` // 函数结构体
}

// PreRegister 预注册函数签名 (用于支持相互递归)
func (f *FunctionStmt) PreRegister(ctx *ValidContext) (*ValidStruct, bool) {
	var structType *ValidStruct
	fnName := f.Name
	isMethod := false

	// 检测 __method_ 前缀
	if strings.HasPrefix(string(f.Name), "__method_") {
		parts := strings.SplitN(strings.TrimPrefix(string(f.Name), "__method_"), "_", 2)
		if len(parts) == 2 {
			typeName := Ident(parts[0])
			methodName := Ident(parts[1])

			// 查找对应的 Struct
			st, ok := ctx.root.structs[typeName]
			if ok {
				structType = st
				fnName = methodName
				isMethod = true
			}
		}
	}

	if !isMethod {
		structType = ctx.root.Global
		fnName = f.Name
	}

	if f.Name == "" {
		ctx.AddErrorf("函数定义缺少名称")
		return nil, false
	}

	if !f.Name.Valid(ctx) {
		return nil, false
	}

	// 验证并解析函数类型
	f.FunctionType.Return = f.FunctionType.Return.Resolve(ctx)
	if !f.FunctionType.Return.Valid(ctx) {
		return nil, false
	}

	for i, param := range f.Params {
		f.Params[i].Type = param.Type.Resolve(ctx)
		if !f.Params[i].Type.Valid(ctx) {
			return nil, false
		}
	}

	// 注册函数签名
	sig := f.FunctionType.ToCallFunctionType()
	if isMethod {
		methodSig := f.FunctionType
		if len(methodSig.Params) > 0 {
			methodSig.Params = append([]FunctionParam(nil), methodSig.Params[1:]...)
		}
		sig = methodSig.ToCallFunctionType()
	}

	if t, ok := structType.Methods[fnName]; ok {
		if t.String() != sig.String() {
			ctx.AddErrorf("函数 %s 已被定义为 %s (新定义: %s)", f.Name, t, sig)
			return nil, false
		}
		return structType, true
	}

	structType.Methods[fnName] = sig

	return structType, true
}

func (f *FunctionStmt) GetBase() *BaseNode { return &f.BaseNode }
func (f *FunctionStmt) stmtNode()          {}

func (f *FunctionStmt) Check(ctx *SemanticContext) error {
	// 注意：PreRegister 必须在此之前由 ProgramStmt.Check 调用过。

	funcCtx := NewSemanticContext(ctx.Child(f))
	// 函数注册应该是全局注册
	funcCtx.parent = nil

	// 1. 检查参数有效性
	for _, param := range f.Params {
		if param.Name == "" || !param.Name.Valid(&funcCtx.ValidContext) {
			return fmt.Errorf("invalid param name: %s", param.Name)
		}
		if param.Type.IsVoid() {
			return fmt.Errorf("%s 不接受 void 类型作为函数参数", param.Name)
		}
	}

	// 2. 创建函数作用域并添加参数
	bodyCtx := funcCtx.Child(f.Body)
	for _, param := range f.Params {
		if param.Name != "" {
			bodyCtx.AddVariable(param.Name, param.Type)
		}
	}

	// 3. 注册到程序中
	f.Type = "Void"
	ctx.root.program.Functions[f.Name] = f

	// 4. 校验函数体
	semBodyCtx := NewSemanticContext(bodyCtx)
	if err := f.Body.Check(semBodyCtx); err != nil {
		return err
	}

	// 5. 返回路径 analysis
	returnTypes, _ := f.FunctionType.Return.ReadTuple()
	if len(returnTypes) > 0 && !(len(returnTypes) == 1 && returnTypes[0].IsVoid()) {
		analyzer := NewReturnAnalyzer(bodyCtx, f)
		if !analyzer.Analyze(f.Body) {
			analyzer.AddReturnPathErrorsToContext(&funcCtx.ValidContext)
			return fmt.Errorf("函数 %s 缺少返回语句", f.Name)
		}
	}

	return nil
}

func (f *FunctionStmt) Optimize(ctx *OptimizeContext) Node {
	f.Body = f.Body.Optimize(ctx).(*BlockStmt)
	f.Body.Inner = true
	return f
}

// MultiAssignmentStmt 表示多变量解构赋值语句
type MultiAssignmentStmt struct {
	BaseNode
	LHS   []Expr `json:"lhs"`
	Value Expr   `json:"value"`
}

func (m *MultiAssignmentStmt) GetBase() *BaseNode { return &m.BaseNode }
func (m *MultiAssignmentStmt) stmtNode()          {}

func (m *MultiAssignmentStmt) Check(ctx *SemanticContext) error {
	m.Type = "Void"
	if len(m.LHS) == 0 {
		return errors.New("multi assignment missing LHS")
	}
	if m.Value == nil {
		return errors.New("multi assignment missing value")
	}

	if err := m.Value.Check(ctx); err != nil {
		return err
	}

	valType := m.Value.GetBase().Type
	var elementTypes []GoMiniType

	if valType.IsTuple() {
		elementTypes, _ = valType.ReadTuple()
	} else if valType.IsArray() {
		// If it's an array, we can't statically know the length,
		// but we know all elements have the same type.
		// For simplicity, we allow destructuring if LHS count is known.
		itemType, _ := valType.ReadArrayItemType()
		for i := 0; i < len(m.LHS); i++ {
			elementTypes = append(elementTypes, GoMiniType(itemType))
		}
	} else if valType.IsResult() {
		// Result decomposes into [val, err]
		resType, _ := valType.ReadResult()
		elementTypes = []GoMiniType{resType, "String"}
	} else if valType.IsAny() {
		for i := 0; i < len(m.LHS); i++ {
			elementTypes = append(elementTypes, "Any")
		}
	} else {
		return fmt.Errorf("cannot destructure non-composite type: %s", valType)
	}

	if len(m.LHS) != len(elementTypes) {
		return fmt.Errorf("assignment count mismatch: %d = %d", len(m.LHS), len(elementTypes))
	}

	for i, lhs := range m.LHS {
		targetType := elementTypes[i]

		if ident, ok := lhs.(*IdentifierExpr); ok {
			ident.Name = ident.Name.Resolve(&ctx.ValidContext)
			vType, exists := ctx.GetVariable(ident.Name)

			if !exists {
				// Type inference for new variables
				if !targetType.IsVoid() {
					ctx.AddVariable(ident.Name, targetType)
					ident.Type = targetType
				}
			} else {
				if !targetType.IsAssignableTo(vType) {
					return fmt.Errorf("type mismatch at index %d: cannot assign %s to %s (%s)", i, targetType, ident.Name, vType)
				}
				if vType == "Any" && targetType != "Any" {
					ctx.UpdateVariable(ident.Name, targetType)
				}
				ident.Type = targetType
			}
		} else {
			if err := lhs.Check(ctx); err != nil {
				return err
			}
			lhsType := lhs.GetBase().Type
			if !targetType.IsAssignableTo(lhsType) {
				return fmt.Errorf("assignment type mismatch at index %d: LHS is %s, RHS is %s", i, lhsType, targetType)
			}
		}
	}

	return nil
}

func (m *MultiAssignmentStmt) Optimize(ctx *OptimizeContext) Node {
	for i, lhs := range m.LHS {
		m.LHS[i] = lhs.Optimize(ctx).(Expr)
	}
	m.Value = m.Value.Optimize(ctx).(Expr)
	return m
}

// GenDeclStmt 变量声明
type GenDeclStmt struct {
	BaseNode
	Name Ident
	Kind GoMiniType
}

func (g *GenDeclStmt) GetBase() *BaseNode { return &g.BaseNode }
func (g *GenDeclStmt) stmtNode()          {}
func (g *GenDeclStmt) Check(ctx *SemanticContext) error {
	g.Type = "Void"
	g.Name = g.Name.Resolve(&ctx.ValidContext)

	// 处理顶级变量命名空间
	if ctx.parent == nil && ctx.root.Package != "" && ctx.root.Package != "main" {
		if !strings.Contains(string(g.Name), ".") {
			g.Name = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, g.Name))
		}
	}

	if !g.Name.Valid(&ctx.ValidContext) {
		return fmt.Errorf("invalid identifier: %s", g.Name)
	}
	g.Kind = g.Kind.Resolve(&ctx.ValidContext)
	if !g.Kind.Valid(&ctx.ValidContext) {
		return fmt.Errorf("invalid type: %s", g.Kind)
	}
	if ctx.IsLocalVariable(g.Name) {
		return fmt.Errorf("variable %s already exists in current scope", g.Name)
	}
	ctx.AddVariable(g.Name, g.Kind)
	return nil
}

func (g *GenDeclStmt) Optimize(ctx *OptimizeContext) Node {
	return g
}

// AssignmentStmt 表示赋值语句
type AssignmentStmt struct {
	BaseNode
	LHS   Expr `json:"lhs"`
	Value Expr `json:"value"`
}

func (a *AssignmentStmt) GetBase() *BaseNode { return &a.BaseNode }
func (a *AssignmentStmt) stmtNode()          {}

// DeferStmt, RangeStmt, SwitchStmt are defined earlier

func (a *AssignmentStmt) Check(ctx *SemanticContext) error {
	a.Type = "Void"
	if a.LHS == nil {
		return errors.New("赋值语句缺少左值")
	}
	if a.Value == nil {
		return errors.New("赋值语句缺少值")
	}

	// 特殊处理左值为 IdentifierExpr，因为可能涉及隐式声明
	if ident, ok := a.LHS.(*IdentifierExpr); ok {
		ident.Name = ident.Name.Resolve(&ctx.ValidContext)

		vType, b := ctx.GetVariable(ident.Name)
		if !b && !strings.Contains(string(ident.Name), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
			mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, ident.Name))
			if vt, ok := ctx.GetVariable(mangled); ok {
				ident.Name = mangled
				vType = vt
				b = true
			}
		}

		if err := a.Value.Check(ctx); err != nil {
			return err
		}
		miniType := a.Value.GetBase().Type
		if miniType.IsEmpty() {
			return errors.New("无法推导类型")
		}
		if miniType.IsVoid() {
			return fmt.Errorf("类型 (%s) 不支持赋值", miniType)
		}

		if b {
			if !miniType.IsAssignableTo(vType) {
				return fmt.Errorf("类型不匹配: 无法将 %s 赋值给 %s (%s)", miniType, ident.Name, vType)
			}
			if vType == "Any" && miniType != "Any" {
				ctx.UpdateVariable(ident.Name, miniType)
			}
			// Update the identifier's own type so subsequent uses in the AST might benefit,
			// though typical Check flows rely on GetVariable.
			ident.Type = miniType
			return nil
		}

		if ctx.parent == nil && ctx.root.Package != "" && ctx.root.Package != "main" {
			if !strings.Contains(string(ident.Name), ".") {
				ident.Name = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, ident.Name))
			}
		}
		ctx.AddVariable(ident.Name, miniType)
		return nil
	}

	// 对于其他复杂的 LHS (IndexExpr, MemberExpr)，直接进行类型检查
	if err := a.LHS.Check(ctx); err != nil {
		return err
	}
	if err := a.Value.Check(ctx); err != nil {
		return err
	}

	lhsType := a.LHS.GetBase().Type
	valType := a.Value.GetBase().Type
	if !valType.IsAssignableTo(lhsType) {
		return fmt.Errorf("赋值类型不匹配: 左值类型为 %s，右值类型为 %s", lhsType, valType)
	}

	return nil
}

func (a *AssignmentStmt) Optimize(ctx *OptimizeContext) Node {
	a.LHS = a.LHS.Optimize(ctx).(Expr)
	a.Value = a.Value.Optimize(ctx).(Expr)

	lhsType := a.LHS.GetBase().Type
	valType := a.Value.GetBase().Type

	if !lhsType.Equals(valType) {
		if ptr, ok := lhsType.AutoPtr(a.Value); ok {
			a.Value = ptr
		}
	}

	return a
}

// InterruptStmt 表示中断语句（break/continue）
type InterruptStmt struct {
	BaseNode
	InterruptType string `json:"interrupt_type"` // "break" 或 "continue"
}

func (i *InterruptStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *InterruptStmt) stmtNode()          {}

func (i *InterruptStmt) Check(ctx *SemanticContext) error {
	if i.InterruptType != "break" && i.InterruptType != "continue" {
		return fmt.Errorf("无效的中断类型: %s", i.InterruptType)
	}

	if _, ok := ctx.CheckScope("for"); !ok {
		return fmt.Errorf("%s 语句只能在循环中使用", i.InterruptType)
	}

	i.Type = "Void"
	return nil
}

func (i *InterruptStmt) Optimize(ctx *OptimizeContext) Node {
	return i
}

// StructStmt 所有 struct 都注册到全局
type StructStmt struct {
	BaseNode
	Name       Ident                `json:"name"`
	Fields     map[Ident]GoMiniType `json:"fields"`
	FieldNames []Ident              `json:"field_names,omitempty"`
}

// PreRegister 预注册结构体 (用于支持相互引用)
func (s *StructStmt) PreRegister(ctx *ValidContext) bool {
	if !s.Name.Valid(ctx) {
		return false
	}

	// 提前注册一个空结构体，以支持自引用或循环引用
	if v, ok := ctx.root.structs[s.Name]; ok {
		// 检查是否已经定义了字段 (如果是 PreRegister 占位，Fields 通常为空)
		// 这里简单处理：如果已经有字段了，说明重复定义了
		if len(v.Fields) > 0 || len(v.Methods) > 0 {
			ctx.AddErrorf("struct %s 已被定义", s.Name)
			return false
		}
		return true
	}

	ctx.root.structs[s.Name] = &ValidStruct{
		Fields:  make(map[Ident]GoMiniType),
		Methods: make(map[Ident]CallFunctionType),
	}
	return true
}

func (s *StructStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *StructStmt) stmtNode()          {}

func (s *StructStmt) Check(ctx *SemanticContext) error {
	// 注意：PreRegister 必须在此之前由 ProgramStmt.Check 调用过。

	// 1. 检查是否已经完全定义过，防止重复定义
	if v, ok := ctx.root.structs[s.Name]; ok {
		if v.Defined {
			return fmt.Errorf("struct %s 已被定义", s.Name)
		}
	}

	// 2. 遍历字段，进行类型解析与合法性检查
	if len(s.FieldNames) == 0 {
		for fieldName := range s.Fields {
			s.FieldNames = append(s.FieldNames, fieldName)
		}
		sort.Slice(s.FieldNames, func(i, j int) bool {
			return s.FieldNames[i] < s.FieldNames[j]
		})
	}

	for _, fieldName := range s.FieldNames {
		fieldType := s.Fields[fieldName]
		s.Fields[fieldName] = fieldType.Resolve(&ctx.ValidContext)
		if !fieldName.Valid(&ctx.ValidContext) {
			return fmt.Errorf("invalid field name: %s", fieldName)
		}
		if !s.Fields[fieldName].Valid(&ctx.ValidContext) {
			return fmt.Errorf("invalid field type for %s: %s", fieldName, fieldType)
		}
	}

	// 3. 填充定义到上下文
	if err := ctx.AddStructDefine(s.Name, s.Fields); err != nil {
		return fmt.Errorf("定义struct失败: %v", err)
	}
	ctx.root.structs[s.Name].Defined = true

	s.Type = "Void"
	ctx.root.program.Structs[s.Name] = s
	return nil
}

func (s *StructStmt) Optimize(ctx *OptimizeContext) Node {
	return s
}
