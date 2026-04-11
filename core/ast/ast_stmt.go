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
	BaseNode   `json:",inline"`
	Package    string                   `json:"package,omitempty"` // 包名，默认为main
	Imports    []ImportSpec             `json:"imports,omitempty"` // 导入列表
	Constants  map[string]string        `json:"constants"`         // 常量表
	Variables  map[Ident]Expr           `json:"variables"`         // 声明的全局变量
	Types      map[Ident]GoMiniType     `json:"types"`             // 命名类型定义 (type MyInt int64)
	Structs    map[Ident]*StructStmt    `json:"structs"`           // 声明的对象 (对象)
	Interfaces map[Ident]*InterfaceStmt `json:"interfaces"`        // 声明的接口
	Functions  map[Ident]*FunctionStmt  `json:"functions"`         // 声明的函数 (解构为无作用域函数)
	Main       []Stmt                   `json:"main"`              // 入口点 （如果没有内容则代表为 lib）
}

// InterfaceStmt 表示接口定义
type InterfaceStmt struct {
	BaseNode `json:",inline"`
	Name     Ident      `json:"name"`
	Type     GoMiniType `json:"type"` // "interface{...}"
}

func (i *InterfaceStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *InterfaceStmt) stmtNode()          {}

func (i *InterfaceStmt) Check(ctx *SemanticContext) error {
	if !i.Type.IsValid() {
		return fmt.Errorf("invalid interface type: %s", i.Type)
	}
	return nil
}

func (i *InterfaceStmt) Optimize(ctx *OptimizeContext) Node { return i }

func (p *ProgramStmt) GetBase() *BaseNode {
	return &p.BaseNode
}
func (p *ProgramStmt) stmtNode() {}

func (p *ProgramStmt) Check(ctx *SemanticContext) error {
	var hasError bool

	// 处理模块加载
	if p.Imports != nil {
		for _, imp := range p.Imports {
			if err := ctx.ImportPackage(imp.Path); err != nil {
				ctx.AddErrorf("%s", err.Error())
				hasError = true
			}
		}
	}

	// 预注册所有导入的包别名
	for alias := range ctx.root.Imports {
		ctx.root.vars[Ident(alias)] = "Package"
	}

	// 第一遍：预注册所有结构体
	for _, structDef := range p.Structs {
		structDef.GetBase().EnsureID(ctx.ValidContext)
		if !structDef.PreRegister(ctx.ValidContext) {
			ctx.AddErrorf("struct %s pre-registration failed", structDef.Name)
			hasError = true
		}
	}
	for _, stmt := range p.Main {
		if s, ok := stmt.(*StructStmt); ok {
			s.GetBase().EnsureID(ctx.ValidContext)
			if !s.PreRegister(ctx.ValidContext) {
				ctx.AddErrorf("struct %s pre-registration failed", s.Name)
				hasError = true
			}
		}
	}

	// 第二遍：预注册所有函数签名
	for name, function := range p.Functions {
		function.GetBase().EnsureID(ctx.ValidContext)
		if _, ok := function.PreRegister(ctx.ValidContext); !ok {
			ctx.AddErrorf("function %s pre-registration failed", name)
			hasError = true
		}
	}
	for _, stmt := range p.Main {
		if f, ok := stmt.(*FunctionStmt); ok {
			f.GetBase().EnsureID(ctx.ValidContext)
			if _, ok := f.PreRegister(ctx.ValidContext); !ok {
				ctx.AddErrorf("function %s pre-registration failed", f.Name)
				hasError = true
			}
		}
	}

	// 第三遍：全量语义校验
	for _, structDef := range p.Structs {
		logCount := ctx.LogCount()
		if err := structDef.Check(ctx); ForwardStructuredError(ctx, structDef, logCount, err) {
			hasError = true
		}
	}

	varOrder, orderErr := p.GlobalInitOrder()
	if orderErr != nil {
		ctx.AddErrorf("%s", orderErr.Error())
		hasError = true
		varOrder = p.DeclaredGlobalOrder()
	}

	for _, i := range varOrder {
		stmt := p.Variables[i]
		if !i.Valid(ctx.ValidContext) {
			ctx.AddErrorf("invalid identifier: %s", i)
			hasError = true
			continue
		}
		if stmt != nil {
			logCount := ctx.LogCount()
			if err := stmt.Check(ctx); ForwardStructuredError(ctx, stmt, logCount, err) {
				hasError = true
			}
			ctx.root.Global.Fields[i] = stmt.GetBase().Type
			ctx.AddVariable(i, stmt.GetBase().Type)
		} else {
			ctx.root.Global.Fields[i] = "Any"
			ctx.AddVariable(i, "Any")
		}
	}

	for _, function := range p.Functions {
		logCount := ctx.LogCount()
		if err := function.Check(ctx); ForwardStructuredError(ctx, function, logCount, err) {
			hasError = true
		}
	}

	for _, node := range p.Main {
		logCount := ctx.LogCount()
		if err := node.Check(ctx); ForwardStructuredError(ctx, node, logCount, err) {
			hasError = true
		}
	}
	if hasError || len(ctx.Logs()) > 0 {
		return &MiniAstError{
			Err:  errors.New("semantic validation failed"),
			Logs: ctx.Logs(),
			Node: p,
		}
	}
	return nil
}

func (p *ProgramStmt) Optimize(ctx *OptimizeContext) Node {
	// 1. 优化结构体定义
	for i, structDef := range p.Structs {
		if structDef != nil {
			opt := structDef.Optimize(ctx)
			p.Structs[i] = opt.(*StructStmt)
		}
	}

	// 2. 优化全局变量定义
	newVars := make(map[Ident]Expr)
	for i, stmt := range p.Variables {
		if stmt != nil {
			if opt := stmt.Optimize(ctx); opt != nil {
				if expr, ok := opt.(Expr); ok {
					newVars[i] = expr
				} else {
					newVars[i] = nil
				}
			} else {
				newVars[i] = nil
			}
		} else {
			newVars[i] = nil
		}
	}
	p.Variables = newVars

	// 3. 优化函数定义
	for i, funcs := range p.Functions {
		if funcs != nil {
			opt := funcs.Optimize(ctx)
			p.Functions[i] = opt.(*FunctionStmt)
		}
	}

	// 4. 处理 Main 块中的语句，并执行定义提取
	var newMain []Stmt
	for _, node := range p.Main {
		if node == nil {
			continue
		}
		optimized := node.Optimize(ctx)
		if optimized == nil {
			continue
		}

		// 如果是 FunctionStmt 或 StructStmt，将其移至全局表并从 Main 中移除
		if fn, ok := optimized.(*FunctionStmt); ok {
			key := fn.Name
			if fn.ReceiverType != "" {
				key = Ident(string(fn.ReceiverType) + "." + string(fn.Name))
			}
			p.Functions[key] = fn
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
	_ = encoder.Encode(p)
	return buffer.String()
}

// DeclaredGlobalOrder returns top-level variable names in declaration order.
// Imports are treated as synthetic globals and come before var declarations.
func (p *ProgramStmt) DeclaredGlobalOrder() []Ident {
	seen := make(map[Ident]struct{})
	order := make([]Ident, 0, len(p.Variables))

	add := func(name Ident) {
		if name == "" {
			return
		}
		if _, ok := p.Variables[name]; !ok {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		order = append(order, name)
	}

	for _, imp := range p.Imports {
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		add(Ident(alias))
	}

	for _, stmt := range p.Main {
		decl, ok := stmt.(*GenDeclStmt)
		if ok {
			add(decl.Name)
		}
	}

	remaining := make([]Ident, 0, len(p.Variables)-len(order))
	for name := range p.Variables {
		if _, ok := seen[name]; !ok {
			remaining = append(remaining, name)
		}
	}

	sort.Slice(remaining, func(i, j int) bool {
		li, lj := p.globalDeclLess(remaining[i], remaining[j])
		if li != lj {
			return li
		}
		return remaining[i] < remaining[j]
	})

	return append(order, remaining...)
}

// GlobalInitOrder resolves package-level initialization order.
// It preserves declaration order where possible, but forces dependencies first.
func (p *ProgramStmt) GlobalInitOrder() ([]Ident, error) {
	declared := p.DeclaredGlobalOrder()
	order := make([]Ident, 0, len(declared))
	state := make(map[Ident]byte, len(declared))

	var visit func(name Ident) error
	visit = func(name Ident) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("circular dependency detected in global initialization: %s", name)
		case 2:
			return nil
		}

		state[name] = 1
		for dep := range p.globalDependencies(name) {
			if dep == name {
				return fmt.Errorf("self dependency detected in global initialization: %s", name)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, name)
		return nil
	}

	for _, name := range declared {
		if err := visit(name); err != nil {
			return declared, err
		}
	}
	return order, nil
}

func (p *ProgramStmt) globalDeclLess(a, b Ident) (bool, bool) {
	exprA := p.Variables[a]
	exprB := p.Variables[b]
	if exprA == nil || exprB == nil {
		return false, false
	}
	locA := exprA.GetBase().Loc
	locB := exprB.GetBase().Loc
	if locA == nil || locB == nil {
		return false, false
	}
	if locA.L != locB.L {
		return locA.L < locB.L, true
	}
	if locA.C != locB.C {
		return locA.C < locB.C, true
	}
	return false, false
}

func (p *ProgramStmt) globalDependencies(name Ident) map[Ident]struct{} {
	deps := make(map[Ident]struct{})
	expr := p.Variables[name]
	p.collectGlobalDependencies(expr, deps)
	delete(deps, name)
	return deps
}

func (p *ProgramStmt) collectGlobalDependencies(expr Expr, deps map[Ident]struct{}) {
	if expr == nil {
		return
	}

	switch n := expr.(type) {
	case *IdentifierExpr:
		if _, ok := p.Variables[n.Name]; ok {
			deps[n.Name] = struct{}{}
		}
	case *StarExpr:
		p.collectGlobalDependencies(n.X, deps)
	case *TypeAssertExpr:
		p.collectGlobalDependencies(n.X, deps)
	case *CallExprStmt:
		if _, ok := n.Func.(*ConstRefExpr); !ok {
			p.collectGlobalDependencies(n.Func, deps)
		}
		for _, arg := range n.Args {
			p.collectGlobalDependencies(arg, deps)
		}
	case *MemberExpr:
		p.collectGlobalDependencies(n.Object, deps)
	case *IndexExpr:
		p.collectGlobalDependencies(n.Object, deps)
		p.collectGlobalDependencies(n.Index, deps)
	case *SliceExpr:
		p.collectGlobalDependencies(n.X, deps)
		p.collectGlobalDependencies(n.Low, deps)
		p.collectGlobalDependencies(n.High, deps)
	case *CompositeExpr:
		isMap := GoMiniType(n.Kind).IsMap()
		for _, elem := range n.Values {
			if elem.Key != nil {
				if isMap {
					p.collectGlobalDependencies(elem.Key, deps)
				} else if _, ok := elem.Key.(*IdentifierExpr); !ok {
					p.collectGlobalDependencies(elem.Key, deps)
				}
			}
			p.collectGlobalDependencies(elem.Value, deps)
		}
	case *BinaryExpr:
		p.collectGlobalDependencies(n.Left, deps)
		p.collectGlobalDependencies(n.Right, deps)
	case *UnaryExpr:
		p.collectGlobalDependencies(n.Operand, deps)
	case *FuncLitExpr, *LiteralExpr, *ConstRefExpr, *ImportExpr, *BadExpr:
		return
	}
}

// BlockStmt 表示代码块或作用域
type BlockStmt struct {
	BaseNode `json:",inline"`
	Children []Stmt `json:"children"`
	Inner    bool   `json:"inner,omitempty"` // 是否开启新作用域
}

func NewBlock(node Node, args ...Stmt) *BlockStmt {
	id := "block"
	var loc *Position
	if node != nil {
		id = node.GetBase().ID
		loc = node.GetBase().Loc
	}
	if len(args) == 0 {
		args = []Stmt{}
	}
	return &BlockStmt{
		BaseNode: BaseNode{
			ID:   id,
			Meta: "block",
			Type: "Void",
			Loc:  loc,
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

	semCtx := ctx
	if !b.Inner {
		semCtx = ctx.Child(b)
	}

	var hasError bool
	for _, child := range b.Children {
		logCount := semCtx.LogCount()
		if err := child.Check(semCtx); ForwardStructuredError(semCtx, child, logCount, err) {
			hasError = true
		}
	}
	if hasError {
		return errors.New("block validation failed")
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
			key := fn.Name
			if fn.ReceiverType != "" {
				key = Ident(string(fn.ReceiverType) + "." + string(fn.Name))
			}
			ctx.root.program.Functions[key] = fn
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
	semCtx := ctx.WithNode(i)
	var hasError bool

	// 1. 检查 Cond
	if i.Cond == nil {
		err := errors.New("if语句缺少条件表达式")
		semCtx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		logCount := semCtx.LogCount()
		if err := i.Cond.Check(semCtx); ForwardStructuredError(semCtx, i.Cond, logCount, err) {
			hasError = true
		} else {
			condType := i.Cond.GetBase().Type
			if condType == "" {
				err := errors.New("if条件表达式类型无法推导")
				semCtx.AddErrorAt(i.Cond, "%s", err.Error())
				hasError = true
			} else if !condType.Equals("Bool") {
				err := fmt.Errorf("if表达式不是返回Bool类型, 实际为 %s", condType)
				semCtx.AddErrorAt(i.Cond, "%s", err.Error())
				hasError = true
			}
		}
	}

	// 2. 检查 Body
	if i.Body == nil {
		err := errors.New("if语句缺少主体")
		semCtx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		if err := i.Body.Check(semCtx); err != nil {
			hasError = true
		}
	}

	// 3. 检查 ElseBody
	if i.ElseBody != nil {
		if err := i.ElseBody.Check(semCtx); err != nil {
			hasError = true
		}
	}

	if hasError {
		return errors.New("if statement validation failed")
	}
	return nil
}

func (i *IfStmt) Optimize(ctx *OptimizeContext) Node {
	// 1. Optimize Cond。
	if i.Cond != nil {
		if opt := i.Cond.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				i.Cond = val
			}
		}
	}

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
	if i.Body != nil {
		opt := i.Body.Optimize(ctx)
		if val, ok := opt.(*BlockStmt); ok {
			i.Body = val
		}
	}
	if i.ElseBody != nil {
		optimizedElse := i.ElseBody.Optimize(ctx)
		i.ElseBody = optimizedElse.(*BlockStmt)
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
	semCtx := ctx.WithNode(f)
	var hasError bool

	if f.Init != nil {
		if err := f.Init.Check(semCtx); err != nil {
			hasError = true
		}
	}

	if f.Cond != nil {
		logCount := semCtx.LogCount()
		if err := f.Cond.Check(semCtx); ForwardStructuredError(semCtx, f.Cond, logCount, err) {
			hasError = true
		} else {
			condType := f.Cond.GetBase().Type
			if condType != "" && !condType.Equals("Bool") {
				err := fmt.Errorf("for循环条件必须是Bool类型, 实际为 %s", condType)
				semCtx.AddErrorAt(f.Cond, "%s", err.Error())
				hasError = true
			}
		}
	}

	if f.Update != nil {
		if err := f.Update.Check(semCtx); err != nil {
			hasError = true
		}
	}

	if f.Body == nil {
		err := errors.New("for循环缺少主体")
		semCtx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		if err := f.Body.Check(semCtx); err != nil {
			hasError = true
		}
		if _, ok := f.Body.(*BlockStmt); !ok {
			err := errors.New("循环主体不是 block")
			semCtx.AddErrorf("%s", err.Error())
			hasError = true
		}
	}

	if hasError {
		return errors.New("for statement validation failed")
	}
	f.Type = "Void"
	return nil
}

func (f *ForStmt) Optimize(ctx *OptimizeContext) Node {
	if f.Init != nil {
		if f.Init != nil {
			if opt := f.Init.Optimize(ctx); opt != nil {
				if val, ok := opt.(Stmt); ok {
					f.Init = val
				}
			}
		}
	}
	if f.Cond != nil {
		if f.Cond != nil {
			if opt := f.Cond.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					f.Cond = val
				}
			}
		}
	}
	if f.Update != nil {
		if f.Update != nil {
			if opt := f.Update.Optimize(ctx); opt != nil {
				if val, ok := opt.(Stmt); ok {
					f.Update = val
				}
			}
		}
	}
	if f.Body != nil {
		opt := f.Body.Optimize(ctx)
		{
			if val, ok := opt.(Stmt); ok {
				f.Body = val
			}
		}
	}
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
	ctx = ctx.WithNode(r)
	var hasError bool
	if r.Results == nil {
		r.Results = make([]Expr, 0)
	}

	for _, result := range r.Results {
		resultCtx := ctx.WithNode(result)
		logCount := resultCtx.LogCount()
		if err := result.Check(resultCtx); ForwardStructuredError(ctx, result, logCount, err) {
			hasError = true
		}
	}

	scope, b := ctx.CheckAnyScope("function", "func_lit")
	if !b {
		err := errors.New("return 语句只能在函数中使用")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	var expectedReturn GoMiniType
	if stmt, ok := scope.(*FunctionStmt); ok {
		expectedReturn = stmt.Return
	} else if expr, ok := scope.(*FuncLitExpr); ok {
		expectedReturn = expr.Return
	} else {
		err := errors.New("未知的函数范围类型")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	if expectedReturn.IsVoid() && len(r.Results) != 0 {
		err := errors.New("当前函数不存在返回值")
		ctx.AddErrorf("%s", err.Error())
		return err
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

		if !tType.IsAssignableTo(expectedReturn) {
			err := fmt.Errorf("返回类型错误 (return:%s != function:%s)", expectedReturn, tType)
			if len(r.Results) == 1 {
				ctx.AddErrorAt(r.Results[0], "%s", err.Error())
			} else {
				ctx.AddErrorAt(r, "%s", err.Error())
			}
			return err
		}
	}

	if hasError {
		return errors.New("return statement validation failed")
	}
	r.Type = "Void"
	return nil
}

func (r *ReturnStmt) Optimize(ctx *OptimizeContext) Node {
	for i, result := range r.Results {
		if result != nil {
			if opt := result.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					r.Results[i] = val
				}
			}
		}
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
	ctx = ctx.WithNode(d)
	d.Type = "Void"
	if d.Call == nil {
		err := errors.New("defer 语句缺少调用表达式")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	return d.Call.Check(ctx.WithNode(d.Call))
}

func (d *DeferStmt) Optimize(ctx *OptimizeContext) Node {
	if d.Call != nil {
		if opt := d.Call.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				d.Call = val
			}
		}
	}
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
	var hasError bool
	if r.X == nil {
		err := errors.New("range 语句缺少对象")
		ctx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		if err := r.X.Check(ctx); err != nil {
			hasError = true
		} else {
			objType := r.X.GetBase().Type
			if !objType.IsArray() && !objType.IsMap() && !objType.IsAny() {
				err := fmt.Errorf("range 语句不支持类型 %s", objType)
				ctx.AddErrorf("%s", err.Error())
				hasError = true
			}
		}
	}

	// 即使头部报错，也要尝试注册变量并校验 Body
	objType := GoMiniType("Any")
	if r.X != nil {
		objType = r.X.GetBase().Type
	}

	semCtx := ctx.WithNode(r)
	inner := semCtx.Child(r.Body)
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

	if err := r.Body.Check(inner); err != nil {
		hasError = true
	}

	if hasError {
		return errors.New("range statement validation failed")
	}
	return nil
}

func (r *RangeStmt) Optimize(ctx *OptimizeContext) Node {
	if r.X != nil {
		if opt := r.X.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				r.X = val
			}
		}
	}
	if r.Body != nil {
		opt := r.Body.Optimize(ctx)
		{
			if val, ok := opt.(*BlockStmt); ok {
				r.Body = val
			}
		}
	}
	return r
}

// SwitchStmt 表示 switch 分支语句
type SwitchStmt struct {
	BaseNode
	Init   Stmt
	Assign Stmt       // 用于 v := x.(type) 中的赋值部分 (v := x)
	Tag    Expr       // 对于 Type Switch，Tag 是 x.(type) 中的 x
	Body   *BlockStmt // 包含多个 CaseClause
	IsType bool       // 是否是 Type Switch
}

func (s *SwitchStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *SwitchStmt) stmtNode()          {}

func (s *SwitchStmt) Check(ctx *SemanticContext) error {
	s.Type = "Void"
	semCtx := ctx.WithNode(s)
	inner := semCtx.Child(s.Body)
	var hasError bool

	if s.Init != nil {
		logCount := inner.LogCount()
		if err := s.Init.Check(inner); ForwardStructuredError(inner, s.Init, logCount, err) {
			hasError = true
		}
	}

	if s.Assign != nil {
		logCount := inner.LogCount()
		if err := s.Assign.Check(inner); ForwardStructuredError(inner, s.Assign, logCount, err) {
			hasError = true
		}
	}

	tagType := GoMiniType("Bool")
	if s.Tag != nil {
		logCount := inner.LogCount()
		if err := s.Tag.Check(inner); ForwardStructuredError(inner, s.Tag, logCount, err) {
			hasError = true
		} else {
			tagType = s.Tag.GetBase().Type
		}
	}

	if s.Body == nil {
		err := errors.New("switch 语句缺少主体")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	for _, child := range s.Body.Children {
		clause, ok := child.(*CaseClause)
		if !ok {
			err := fmt.Errorf("switch 语句只能包含 CaseClause, 得到 %T", child)
			ctx.AddErrorf("%s", err.Error())
			hasError = true
			continue
		}
		if s.IsType {
			// 在 Type Switch 中，Case 列表中的表达式应该代表类型
			for _, expr := range clause.List {
				if expr == nil {
					continue
				}
				// 标记为类型校验
				expr.GetBase().IsType = true
				logCount := inner.LogCount()
				if err := expr.Check(inner); ForwardStructuredError(inner, expr, logCount, err) {
					hasError = true
				}
			}
			// 校验 Case 的 Body
			for _, s := range clause.Body {
				logCount := inner.LogCount()
				if err := s.Check(inner); ForwardStructuredError(inner, s, logCount, err) {
					hasError = true
				}
			}
		} else {
			if err := clause.CheckWithTag(inner, tagType); err != nil {
				hasError = true
			}
		}
	}

	if hasError {
		return errors.New("switch statement validation failed")
	}
	return nil
}

func (s *SwitchStmt) Optimize(ctx *OptimizeContext) Node {
	if s.Init != nil {
		if s.Init != nil {
			if opt := s.Init.Optimize(ctx); opt != nil {
				if val, ok := opt.(Stmt); ok {
					s.Init = val
				}
			}
		}
	}
	if s.Assign != nil {
		if s.Assign != nil {
			if opt := s.Assign.Optimize(ctx); opt != nil {
				if val, ok := opt.(Stmt); ok {
					s.Assign = val
				}
			}
		}
	}
	if s.Tag != nil {
		if s.Tag != nil {
			if opt := s.Tag.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					s.Tag = val
				}
			}
		}
	}
	if s.Body != nil {
		if s.Body != nil {
			opt := s.Body.Optimize(ctx)
			{
				if val, ok := opt.(*BlockStmt); ok {
					s.Body = val
				}
			}
		}
	}
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
	var hasError bool
	for i, expr := range c.List {
		if err := expr.Check(ctx); err != nil {
			hasError = true
		} else {
			if !expr.GetBase().Type.IsAssignableTo(tagType) {
				err := fmt.Errorf("case 类型不匹配: 期望 %s, 实际 %s", tagType, expr.GetBase().Type)
				ctx.AddErrorAt(expr, "%s", err.Error())
				hasError = true
			}
			c.List[i] = expr.Optimize(NewOptimizeContext(ctx.ValidContext)).(Expr)
		}
	}
	for i, stmt := range c.Body {
		if err := stmt.Check(ctx); err != nil {
			hasError = true
		} else {
			c.Body[i] = stmt.Optimize(NewOptimizeContext(ctx.ValidContext)).(Stmt)
		}
	}
	if hasError {
		return errors.New("case clause validation failed")
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
	Scope        Ident      `json:"scope,omitempty"`         // 函数的作用域
	Name         Ident      `json:"name"`                    // 函数名
	ReceiverType Ident      `json:"receiver_type,omitempty"` // 接收者类型 (如果是方法)
	Body         *BlockStmt `json:"body"`                    // 函数结构体
	Doc          string     `json:"doc,omitempty"`
}

// PreRegister 预注册函数签名 (用于支持相互递归)
func (f *FunctionStmt) PreRegister(ctx *ValidContext) (*ValidStruct, bool) {
	var structType *ValidStruct
	fnName := f.Name
	isMethod := false

	// 首先检查 ReceiverType 字段
	if f.ReceiverType != "" {
		st, ok := ctx.root.structs[f.ReceiverType]
		if ok {
			structType = st
			isMethod = true
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

	funcCtx := ctx.Child(f)
	// 函数注册应该是全局注册
	funcCtx.parent = nil

	var hasError bool
	// 1. 检查参数有效性
	for _, param := range f.Params {
		if param.Name == "" || !param.Name.Valid(funcCtx.ValidContext) {
			err := fmt.Errorf("invalid param name: %s", param.Name)
			funcCtx.AddErrorf("%s", err.Error())
			hasError = true
		}
		if param.Type.IsVoid() {
			err := fmt.Errorf("%s 不接受 void 类型作为函数参数", param.Name)
			funcCtx.AddErrorf("%s", err.Error())
			hasError = true
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
	semBodyCtx := bodyCtx
	logCount := semBodyCtx.LogCount()
	if err := f.Body.Check(semBodyCtx); ForwardStructuredError(semBodyCtx, f.Body, logCount, err) {
		hasError = true
	}

	// 5. 返回路径 analysis
	retType := f.Return
	if retType != "" && !retType.IsVoid() {
		analyzer := NewReturnAnalyzer(funcCtx.ValidContext, retType)
		if !analyzer.Analyze(f.Body) {
			analyzer.AddReturnPathErrorsToContext(funcCtx.ValidContext)
			if analyzer.ErrorCount() == 0 {
				err := fmt.Errorf("函数 %s 缺少返回语句", f.Name)
				funcCtx.AddErrorAt(f.Body, "%s", err.Error())
			}
			hasError = true
		}
	}

	if hasError {
		return errors.New("function validation failed")
	}
	return nil
}

func (f *FunctionStmt) Optimize(ctx *OptimizeContext) Node {
	if f.Body != nil {
		opt := f.Body.Optimize(ctx)
		{
			if block, ok := opt.(*BlockStmt); ok {
				f.Body = block
				f.Body.Inner = true
			}
		}
	}
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
		err := errors.New("multi assignment missing LHS")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if m.Value == nil {
		err := errors.New("multi assignment missing value")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	if err := m.Value.Check(ctx); err != nil {
		return err
	}

	valType := m.Value.GetBase().Type
	var elementTypes []GoMiniType

	if valType.IsTuple() {
		elementTypes, _ = valType.ReadTuple()
	} else if valType.IsArray() {
		itemType, _ := valType.ReadArrayItemType()
		for i := 0; i < len(m.LHS); i++ {
			elementTypes = append(elementTypes, itemType)
		}
	} else if valType.IsAny() {
		for i := 0; i < len(m.LHS); i++ {
			elementTypes = append(elementTypes, "Any")
		}
	} else {
		err := fmt.Errorf("cannot destructure non-composite type: %s", valType)
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	if len(m.LHS) != len(elementTypes) {
		err := fmt.Errorf("assignment count mismatch: %d = %d", len(m.LHS), len(elementTypes))
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	var hasError bool
	for i, lhs := range m.LHS {
		targetType := elementTypes[i]
		if lhs == nil {
			// Skip check for blank identifier
			continue
		}

		if ident, ok := lhs.(*IdentifierExpr); ok {
			ident.Name = ident.Name.Resolve(ctx.ValidContext)
			vType, exists := ctx.GetVariable(ident.Name)

			if !exists {
				if !targetType.IsVoid() {
					ctx.AddVariable(ident.Name, targetType)
					ident.Type = targetType
				}
			} else {
				if !targetType.IsAssignableTo(vType) {
					err := fmt.Errorf("type mismatch at index %d: cannot assign %s to %s (%s)", i, targetType, ident.Name, vType)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
				}
				if vType == "Any" && targetType != "Any" {
					ctx.UpdateVariable(ident.Name, targetType)
				}
				ident.Type = targetType
			}
		} else {
			if err := lhs.Check(ctx); err != nil {
				hasError = true
			} else {
				lhsType := lhs.GetBase().Type
				if !targetType.IsAssignableTo(lhsType) {
					err := fmt.Errorf("assignment type mismatch at index %d: LHS is %s, RHS is %s", i, lhsType, targetType)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
				}
			}
		}
	}

	if hasError {
		return errors.New("multi assignment validation failed")
	}
	return nil
}

func (m *MultiAssignmentStmt) Optimize(ctx *OptimizeContext) Node {
	for i, lhs := range m.LHS {
		if lhs != nil {
			if opt := lhs.Optimize(ctx); opt != nil {
				if val, ok := opt.(Expr); ok {
					m.LHS[i] = val
				}
			}
		}
	}
	if m.Value != nil {
		if opt := m.Value.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				m.Value = val
			}
		}
	}
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
	ctx = ctx.WithNode(g)
	g.Type = "Void"
	g.Name = g.Name.Resolve(ctx.ValidContext)

	// 处理顶级变量命名空间
	if ctx.parent == nil && ctx.root.Package != "" && ctx.root.Package != "main" {
		if !strings.Contains(string(g.Name), ".") {
			g.Name = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, g.Name))
		}
	}

	if !g.Name.Valid(ctx.ValidContext) {
		err := fmt.Errorf("invalid identifier: %s", g.Name)
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	g.Kind = g.Kind.Resolve(ctx.ValidContext)
	if !g.Kind.Valid(ctx.ValidContext) {
		err := fmt.Errorf("invalid type: %s", g.Kind)
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if ctx.IsLocalVariable(g.Name) {
		if ctx.parent == nil {
			ctx.AddVariable(g.Name, g.Kind)
			return nil
		}
		// Allow "re-declaration" if we are in a local scope to support a, err := f1(); b, err := f2()
		// Go allows this as long as there is at least one new variable.
		// In go-mini, we relax this to always allow re-decl in local scope for now,
		// relying on the executor's idempotency.
		return nil
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
	ctx = ctx.WithNode(a)
	a.Type = "Void"
	if a.LHS == nil {
		err := errors.New("赋值语句缺少左值")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if a.Value == nil {
		err := errors.New("赋值语句缺少值")
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	// 特殊处理左值为 IdentifierExpr，因为可能涉及隐式声明
	if ident, ok := a.LHS.(*IdentifierExpr); ok {
		ident.Name = ident.Name.Resolve(ctx.ValidContext)

		vType, b := ctx.GetVariable(ident.Name)
		if !b && !strings.Contains(string(ident.Name), ".") && ctx.root.Package != "" && ctx.root.Package != "main" {
			mangled := Ident(fmt.Sprintf("%s.%s", ctx.root.Package, ident.Name))
			if vt, ok := ctx.GetVariable(mangled); ok {
				ident.Name = mangled
				vType = vt
				b = true
			}
		}

		if b {
			// 如果左值类型已知且右值是无类型的字面量，预设类型以便推导
			if sub, ok := a.Value.(*CompositeExpr); ok && sub.Kind == "" {
				sub.BaseNode.Type = vType
			}
		}

		if err := a.Value.Check(ctx); err != nil {
			return err
		}
		miniType := a.Value.GetBase().Type
		if miniType.IsEmpty() {
			err := errors.New("无法推导类型")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if miniType.IsVoid() {
			err := fmt.Errorf("类型 (%s) 不支持赋值", miniType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}

		if b {
			if !miniType.IsAssignableTo(vType) {
				err := fmt.Errorf("类型不匹配: 无法将 %s 赋值给 %s (%s)", miniType, ident.Name, vType)
				ctx.AddErrorAt(a.Value, "%s", err.Error())
				return err
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
		err := fmt.Errorf("赋值类型不匹配: 左值类型为 %s，右值类型为 %s", lhsType, valType)
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	return nil
}

func (a *AssignmentStmt) Optimize(ctx *OptimizeContext) Node {
	if a.LHS != nil {
		if opt := a.LHS.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				a.LHS = val
			}
		}
	}
	if a.Value != nil {
		if opt := a.Value.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				a.Value = val
			}
		}
	}

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
	switch i.InterruptType {
	case "break":
		if _, ok := ctx.CheckAnyScope("for", "range", "switch"); !ok {
			err := errors.New("break 语句只能在循环或 switch 中使用")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	case "continue":
		if _, ok := ctx.CheckAnyScope("for", "range"); !ok {
			err := errors.New("continue 语句只能在循环中使用")
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	default:
		err := fmt.Errorf("无效的中断类型: %s", i.InterruptType)
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	i.Type = "Void"
	return nil
}

func (i *InterruptStmt) Optimize(ctx *OptimizeContext) Node {
	return i
}

// TryStmt 表示 try-catch-finally 语句
type TryStmt struct {
	BaseNode `json:",inline"`
	Body     *BlockStmt   `json:"body"`
	Catch    *CatchClause `json:"catch,omitempty"`
	Finally  *BlockStmt   `json:"finally,omitempty"`
}

func (t *TryStmt) GetBase() *BaseNode { return &t.BaseNode }
func (t *TryStmt) stmtNode()          {}

func (t *TryStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(t)
	t.Type = "Void"
	var hasError bool
	if t.Body == nil {
		err := errors.New("try 语句缺少主体")
		ctx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		if err := t.Body.Check(ctx.WithNode(t.Body)); err != nil {
			hasError = true
		}
	}
	if t.Catch != nil {
		inner := ctx.Child(t.Catch).WithNode(t.Catch)
		if t.Catch.VarName != "" {
			inner.AddVariable(t.Catch.VarName, "Any")
		}
		if err := t.Catch.Body.Check(inner.WithNode(t.Catch.Body)); err != nil {
			hasError = true
		}
	}
	if t.Finally != nil {
		if err := t.Finally.Check(ctx.WithNode(t.Finally)); err != nil {
			hasError = true
		}
	}
	if hasError {
		return errors.New("try statement validation failed")
	}
	return nil
}

func (t *TryStmt) Optimize(ctx *OptimizeContext) Node {
	if t.Body != nil {
		opt := t.Body.Optimize(ctx)
		{
			if val, ok := opt.(*BlockStmt); ok {
				t.Body = val
			}
		}
	}
	if t.Catch != nil {
		if t.Catch.Body != nil {
			opt := t.Catch.Body.Optimize(ctx)
			if val, ok := opt.(*BlockStmt); ok {
				t.Catch.Body = val
			}
		}
	}
	if t.Finally != nil {
		if t.Finally != nil {
			opt := t.Finally.Optimize(ctx)
			{
				if val, ok := opt.(*BlockStmt); ok {
					t.Finally = val
				}
			}
		}
	}
	return t
}

// CatchClause 表示 catch 分支
type CatchClause struct {
	BaseNode `json:",inline"`
	VarName  Ident      `json:"var_name,omitempty"`
	Body     *BlockStmt `json:"body"`
}

func (c *CatchClause) GetBase() *BaseNode { return &c.BaseNode }
func (c *CatchClause) stmtNode()          {}
func (c *CatchClause) Check(ctx *SemanticContext) error {
	return errors.New("CatchClause should be checked via TryStmt")
}
func (c *CatchClause) Optimize(ctx *OptimizeContext) Node { return c }

// StructStmt 所有 struct 都注册到全局
type StructStmt struct {
	BaseNode
	Name       Ident                `json:"name"`
	Fields     map[Ident]GoMiniType `json:"fields"`
	FieldNames []Ident              `json:"field_names,omitempty"`
	FieldLocs  map[Ident]*Position  `json:"field_locs,omitempty"`
	Doc        string               `json:"doc,omitempty"`
}

// PreRegister 预注册结构体 (用于支持相互引用)
func (s *StructStmt) PreRegister(ctx *ValidContext) bool {
	if !s.Name.Valid(ctx) {
		return false
	}

	// 提前注册一个空结构体，以支持自引用或循环引用
	if v, ok := ctx.root.structs[s.Name]; ok {
		// 检查是否已经定义了字段 (如果是 PreRegister 占位，Fields 通常为空)
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
	if v, ok := ctx.root.structs[s.Name]; ok {
		if v.Defined {
			err := fmt.Errorf("struct %s 已被定义", s.Name)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	}

	if len(s.FieldNames) == 0 {
		for fieldName := range s.Fields {
			s.FieldNames = append(s.FieldNames, fieldName)
		}
		sort.Slice(s.FieldNames, func(i, j int) bool {
			return s.FieldNames[i] < s.FieldNames[j]
		})
	}

	var hasError bool
	for _, fieldName := range s.FieldNames {
		fieldType := s.Fields[fieldName]
		s.Fields[fieldName] = fieldType.Resolve(ctx.ValidContext)
		if !fieldName.Valid(ctx.ValidContext) {
			err := fmt.Errorf("invalid field name: %s", fieldName)
			ctx.AddErrorf("%s", err.Error())
			hasError = true
		}
		if !s.Fields[fieldName].Valid(ctx.ValidContext) {
			err := fmt.Errorf("invalid field type for %s: %s", fieldName, fieldType)
			ctx.AddErrorf("%s", err.Error())
			hasError = true
		}
	}

	if err := ctx.AddStructDefine(s.Name, s.Fields); err != nil {
		err2 := fmt.Errorf("定义struct失败: %v", err)
		ctx.AddErrorf("%s", err2.Error())
		return err2
	}
	ctx.root.structs[s.Name].Defined = true

	if hasError {
		return errors.New("struct validation failed")
	}

	s.Type = "Void"
	ctx.root.program.Structs[s.Name] = s
	return nil
}

func (s *StructStmt) Optimize(ctx *OptimizeContext) Node {
	return s
}
