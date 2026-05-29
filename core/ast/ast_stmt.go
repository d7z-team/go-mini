package ast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/typespec"
)

type AssignKind string

const (
	AssignSet    AssignKind = "="
	AssignDefine AssignKind = ":="
)

// ImportSpec 表示包导入声明
type ImportSpec struct {
	Alias       string `json:"alias,omitempty"`        // 别名，默认为空表示使用包名
	Path        string `json:"path"`                   // 导入路径
	File        string `json:"file,omitempty"`         // 所属源码文件
	Synthetic   bool   `json:"synthetic,omitempty"`    // 编译期生成的导入
	CompileOnly bool   `json:"compile_only,omitempty"` // 仅供编译期模板解析，不能进入 runtime
}

// ImportLocationKey returns the location-map key for a file-scoped import alias.
func ImportLocationKey(file, alias string) string {
	if file == "" {
		return alias
	}
	return file + "\x1f" + alias
}

// ProgramStmt 程序启动
type ProgramStmt struct {
	BaseNode      `json:",inline"`
	Package       string                   `json:"package,omitempty"` // 包名，默认为main
	ModulePath    string                   `json:"module_path,omitempty"`
	Imports       []ImportSpec             `json:"imports,omitempty"` // 导入列表
	Constants     map[string]string        `json:"constants"`         // 常量表
	ConstantTypes map[string]GoMiniType    `json:"constant_types,omitempty"`
	ConstantLocs  map[string]*Position     `json:"constant_locs,omitempty"`
	Variables     map[Ident]Expr           `json:"variables"` // 声明的全局变量
	Types         map[Ident]GoMiniType     `json:"types"`     // 命名类型定义 (type MyInt int64)
	TypeLocs      map[Ident]*Position      `json:"type_locs,omitempty"`
	Structs       map[Ident]*StructStmt    `json:"structs"`    // 声明的对象 (对象)
	Interfaces    map[Ident]*InterfaceStmt `json:"interfaces"` // 声明的接口
	ImportLocs    map[string]*Position     `json:"import_locs,omitempty"`
	Functions     map[Ident]*FunctionStmt  `json:"functions"` // 声明的函数 (解构为无作用域函数)
	Main          []Stmt                   `json:"main"`      // 入口点 （如果没有内容则代表为 lib）
}

// InterfaceStmt 表示接口定义
type InterfaceStmt struct {
	BaseNode      `json:",inline"`
	Name          Ident      `json:"name"`
	QualifiedName Ident      `json:"qualified_name,omitempty"`
	Type          GoMiniType `json:"type"` // "interface{...}"
}

func (i *InterfaceStmt) GetBase() *BaseNode { return &i.BaseNode }
func (i *InterfaceStmt) stmtNode()          {}

func (i *InterfaceStmt) Check(ctx *SemanticContext) error {
	if ctx != nil {
		i.QualifiedName = ctx.QualifiedTypeName(i.Name)
		i.Type = i.Type.Resolve(ctx.ValidContext)
	}
	return i.Type.ValidateCanonical()
}

func (i *InterfaceStmt) Optimize(_ *OptimizeContext) Node { return i }

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

	for ident, t := range p.Types {
		t = t.Resolve(ctx.ValidContext)
		p.Types[ident] = t
		ctx.root.types[ident] = t
		if err := t.ValidateCanonical(); err != nil {
			ctx.AddErrorf("type %s has %s", ident, err.Error())
			hasError = true
		}
	}

	// 第一遍：预注册所有结构体
	for name, structDef := range p.Structs {
		if structDef == nil {
			ctx.AddErrorf("struct %s is nil", name)
			hasError = true
			continue
		}
		structDef.GetBase().EnsureID(ctx.ValidContext)
		if !structDef.PreRegister(ctx.ValidContext) {
			ctx.AddErrorf("struct %s pre-registration failed", structDef.Name)
			hasError = true
		}
	}
	for _, stmt := range p.Main {
		if stmt == nil {
			ctx.AddErrorf("program main contains nil statement")
			hasError = true
			continue
		}
		if s, ok := stmt.(*StructStmt); ok {
			s.GetBase().EnsureID(ctx.ValidContext)
			if !s.PreRegister(ctx.ValidContext) {
				ctx.AddErrorf("struct %s pre-registration failed", s.Name)
				hasError = true
			}
		}
	}

	// 第二遍：预注册所有函数签名
	normalizedFunctions := make(map[Ident]*FunctionStmt, len(p.Functions))
	for name, function := range p.Functions {
		if function == nil {
			ctx.AddErrorf("function %s is nil", name)
			hasError = true
			continue
		}
		function.GetBase().EnsureID(ctx.ValidContext)
		if _, ok := function.PreRegister(ctx.ValidContext); !ok {
			ctx.AddErrorf("function %s pre-registration failed", name)
			hasError = true
		}
		key := function.RegistryName()
		if existing := normalizedFunctions[key]; existing != nil && existing != function {
			ctx.AddErrorf("duplicate function definition: %s", key)
			hasError = true
			continue
		}
		normalizedFunctions[key] = function
	}
	p.Functions = normalizedFunctions
	for _, stmt := range p.Main {
		if stmt == nil {
			continue
		}
		if f, ok := stmt.(*FunctionStmt); ok {
			f.GetBase().EnsureID(ctx.ValidContext)
			if _, ok := f.PreRegister(ctx.ValidContext); !ok {
				ctx.AddErrorf("function %s pre-registration failed", f.Name)
				hasError = true
			}
			key := f.RegistryName()
			if existing := p.Functions[key]; existing != nil && existing != f {
				ctx.AddErrorf("duplicate function definition: %s", key)
				hasError = true
				continue
			}
			p.Functions[key] = f
		}
	}

	p.SyncTopLevelDeclVariables()
	if err := p.validateTopLevelNamespace(); err != nil {
		ctx.AddErrorf("%s", err.Error())
		hasError = true
	}

	type globalDeclInfo struct {
		kind     GoMiniType
		inferred bool
	}
	globalDecls := make(map[Ident]globalDeclInfo, len(p.Variables))
	for _, stmt := range p.Main {
		if stmt == nil {
			continue
		}
		decl, ok := stmt.(*GenDeclStmt)
		if !ok || decl == nil {
			continue
		}
		infos, ok := decl.resolveBindings(ctx, true)
		if !ok {
			hasError = true
			continue
		}
		for _, info := range infos {
			if info.name == "_" {
				continue
			}
			if _, isGlobal := p.Variables[info.name]; !isGlobal {
				continue
			}
			if _, exists := globalDecls[info.name]; exists {
				ctx.AddErrorf("variable redeclared in this block: %s", info.name)
				hasError = true
				continue
			}
			globalDecls[info.name] = globalDeclInfo{kind: info.kind, inferred: info.inferred}
		}
	}

	// 先登记所有显式全局声明和导入别名；inferred 全局必须等 initializer 推断完成后再进入作用域。
	for _, name := range p.DeclaredGlobalOrder() {
		if !name.Valid(ctx.ValidContext) {
			ctx.AddErrorf("invalid identifier: %s", name)
			hasError = true
			continue
		}
		if decl, ok := globalDecls[name]; ok {
			if decl.inferred {
				continue
			}
			ctx.root.Global.Fields[name] = decl.kind
			ctx.AddVariable(name, decl.kind)
			continue
		}
		if expr := p.Variables[name]; expr != nil {
			if _, ok := expr.(*ImportExpr); ok {
				ctx.root.Global.Fields[name] = TypeModule
				ctx.AddVariable(name, TypeModule)
			}
		}
	}

	// 第三遍：全量语义校验
	for name, structDef := range p.Structs {
		if structDef == nil {
			ctx.AddErrorf("struct %s is nil", name)
			hasError = true
			continue
		}
		logCount := ctx.LogCount()
		if err := structDef.Check(ctx); ForwardStructuredError(ctx, structDef, logCount, err) {
			hasError = true
		}
	}

	for name, iface := range p.Interfaces {
		if iface == nil {
			ctx.AddErrorf("interface %s is nil", name)
			hasError = true
			continue
		}
		logCount := ctx.LogCount()
		if err := iface.Check(ctx); ForwardStructuredError(ctx, iface, logCount, err) {
			hasError = true
		}
	}

	groupOrder, orderErr := p.GlobalInitGroups()
	if orderErr != nil {
		ctx.AddErrorf("%s", orderErr.Error())
		hasError = true
		groupOrder = p.DeclaredGlobalGroups()
	}

	for _, group := range groupOrder {
		if group.Decl != nil {
			infos, ok := group.Decl.resolveBindings(ctx, true)
			if !ok {
				hasError = true
				continue
			}
			finalTypes, ok := group.Decl.resolveValueTypes(ctx, infos)
			if !ok {
				hasError = true
				continue
			}
			for i, info := range infos {
				if info.name == "_" {
					continue
				}
				if _, isGlobal := p.Variables[info.name]; !isGlobal {
					continue
				}
				ctx.root.Global.Fields[info.name] = finalTypes[i]
				ctx.AddVariable(info.name, finalTypes[i])
			}
			continue
		}

		for _, name := range group.Names {
			expr := p.Variables[name]
			finalType := TypeAny
			if expr != nil {
				logCount := ctx.LogCount()
				if err := expr.Check(ctx); ForwardStructuredError(ctx, expr, logCount, err) {
					hasError = true
					continue
				}
				finalType = expr.GetBase().Type
				if finalType.IsEmpty() || finalType.IsVoid() {
					ctx.AddErrorf("global %s initializer has invalid type: %s", name, finalType)
					hasError = true
					continue
				}
			}
			ctx.root.Global.Fields[name] = finalType
			ctx.AddVariable(name, finalType)
		}
	}

	for name, function := range p.Functions {
		if function == nil {
			ctx.AddErrorf("function %s is nil", name)
			hasError = true
			continue
		}
		logCount := ctx.LogCount()
		if err := function.Check(ctx); ForwardStructuredError(ctx, function, logCount, err) {
			hasError = true
		}
	}

	for _, node := range p.Main {
		if node == nil {
			ctx.AddErrorf("program main contains nil statement")
			hasError = true
			continue
		}
		if decl, ok := node.(*GenDeclStmt); ok {
			if p.isGlobalDecl(decl) {
				continue
			}
		}
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

func (p *ProgramStmt) validateTopLevelNamespace() error {
	if p == nil {
		return nil
	}
	seen := make(map[Ident]string)
	importPaths := make(map[Ident]string)
	add := func(name Ident, kind string) error {
		if name == "" || name == "_" {
			return nil
		}
		if existing, ok := seen[name]; ok {
			if existing == kind {
				return nil
			}
			return fmt.Errorf("duplicate top-level symbol %s: %s conflicts with %s", name, kind, existing)
		}
		seen[name] = kind
		return nil
	}
	for _, imp := range p.Imports {
		alias := Ident(imp.Alias)
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = Ident(parts[len(parts)-1])
		}
		if existingPath, ok := importPaths[alias]; ok && existingPath != imp.Path {
			return fmt.Errorf("duplicate import alias %s: %s conflicts with %s", alias, imp.Path, existingPath)
		}
		importPaths[alias] = imp.Path
		if err := add(alias, "import"); err != nil {
			return err
		}
	}
	for name := range p.Constants {
		if err := add(Ident(name), "constant"); err != nil {
			return err
		}
	}
	for name, expr := range p.Variables {
		kind := "variable"
		if _, ok := expr.(*ImportExpr); ok {
			kind = "import"
		}
		if err := add(name, kind); err != nil {
			return err
		}
	}
	for name := range p.Types {
		if err := add(name, "type"); err != nil {
			return err
		}
	}
	for name := range p.Structs {
		if err := add(name, "type"); err != nil {
			return err
		}
	}
	for name := range p.Interfaces {
		if err := add(name, "type"); err != nil {
			return err
		}
	}
	for key, fn := range p.Functions {
		if fn != nil && fn.ReceiverType != "" {
			continue
		}
		if strings.Contains(string(key), ".") {
			continue
		}
		name := key
		if fn != nil && fn.Name != "" {
			name = fn.Name
		}
		if err := add(name, "function"); err != nil {
			return err
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
			p.Functions[fn.RegistryName()] = fn
			continue
		}
		if st, ok := optimized.(*StructStmt); ok {
			p.Structs[st.Name] = st
			continue
		}

		if stmt, ok := optimized.(Stmt); ok {
			newMain = append(newMain, stmt)
		}
	}
	p.Main = newMain
	p.SyncTopLevelDeclVariables()
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

type GlobalDeclGroup struct {
	Names  []Ident
	Values []Expr
	Decl   *GenDeclStmt
}

func (p *ProgramStmt) SyncTopLevelDeclVariables() {
	if p == nil {
		return
	}
	if p.Variables == nil {
		p.Variables = make(map[Ident]Expr)
	}
	for _, stmt := range p.Main {
		decl, ok := stmt.(*GenDeclStmt)
		if !ok || decl == nil {
			continue
		}
		for i, binding := range decl.Bindings {
			name := binding.Name
			if name == "" || name == "_" {
				continue
			}
			p.Variables[name] = decl.valueForBinding(i)
		}
	}
}

func (g *GenDeclStmt) valueForBinding(index int) Expr {
	if g == nil || len(g.Values) == 0 {
		return nil
	}
	if len(g.Values) == len(g.Bindings) && index >= 0 && index < len(g.Values) {
		return g.Values[index]
	}
	if len(g.Values) == 1 {
		return g.Values[0]
	}
	return nil
}

func (p *ProgramStmt) isGlobalDecl(decl *GenDeclStmt) bool {
	if p == nil || decl == nil {
		return false
	}
	for _, binding := range decl.Bindings {
		if binding.Name == "" || binding.Name == "_" {
			continue
		}
		if _, ok := p.Variables[binding.Name]; ok {
			return true
		}
	}
	return false
}

func (p *ProgramStmt) DeclaredGlobalGroups() []GlobalDeclGroup {
	if p == nil {
		return nil
	}
	seen := make(map[Ident]struct{})
	groups := make([]GlobalDeclGroup, 0, len(p.Variables))
	addSingle := func(name Ident) {
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
		var values []Expr
		if expr := p.Variables[name]; expr != nil {
			values = []Expr{expr}
		}
		groups = append(groups, GlobalDeclGroup{Names: []Ident{name}, Values: values})
	}

	for _, imp := range p.Imports {
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		addSingle(Ident(alias))
	}

	for _, stmt := range p.Main {
		decl, ok := stmt.(*GenDeclStmt)
		if !ok || decl == nil {
			continue
		}
		names := make([]Ident, 0, len(decl.Bindings))
		for _, binding := range decl.Bindings {
			name := binding.Name
			if name == "" || name == "_" {
				continue
			}
			if _, ok := p.Variables[name]; !ok {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		if len(names) > 0 {
			groups = append(groups, GlobalDeclGroup{Names: names, Values: decl.Values, Decl: decl})
		}
	}

	remaining := make([]Ident, 0, len(p.Variables))
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
	for _, name := range remaining {
		addSingle(name)
	}
	return groups
}

// DeclaredGlobalOrder returns top-level variable names in declaration order.
// Imports are treated as synthetic globals and come before var declarations.
func (p *ProgramStmt) DeclaredGlobalOrder() []Ident {
	order := make([]Ident, 0, len(p.Variables))
	for _, group := range p.DeclaredGlobalGroups() {
		order = append(order, group.Names...)
	}
	return order
}

func (p *ProgramStmt) GlobalInitGroups() ([]GlobalDeclGroup, error) {
	declared := p.DeclaredGlobalGroups()
	nameToGroup := make(map[Ident]int, len(p.Variables))
	for i, group := range declared {
		for _, name := range group.Names {
			nameToGroup[name] = i
		}
	}

	order := make([]GlobalDeclGroup, 0, len(declared))
	state := make(map[int]byte, len(declared))

	var visit func(index int) error
	visit = func(index int) error {
		switch state[index] {
		case 1:
			names := make([]string, len(declared[index].Names))
			for i, name := range declared[index].Names {
				names[i] = string(name)
			}
			return fmt.Errorf("circular dependency detected in global initialization: %s", strings.Join(names, ","))
		case 2:
			return nil
		}

		state[index] = 1
		for dep := range p.globalDependenciesForValues(declared[index].Values) {
			depIndex, ok := nameToGroup[dep]
			if !ok {
				continue
			}
			if depIndex == index {
				return fmt.Errorf("self dependency detected in global initialization: %s", dep)
			}
			if err := visit(depIndex); err != nil {
				return err
			}
		}
		state[index] = 2
		order = append(order, declared[index])
		return nil
	}

	for i := range declared {
		if err := visit(i); err != nil {
			return declared, err
		}
	}
	return order, nil
}

// GlobalInitOrder resolves package-level initialization order.
// It preserves declaration order where possible, but forces dependencies first.
func (p *ProgramStmt) GlobalInitOrder() ([]Ident, error) {
	groups, err := p.GlobalInitGroups()
	if err != nil {
		return p.DeclaredGlobalOrder(), err
	}
	order := make([]Ident, 0, len(p.Variables))
	for _, group := range groups {
		order = append(order, group.Names...)
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

func (p *ProgramStmt) globalDependenciesForValues(values []Expr) map[Ident]struct{} {
	deps := make(map[Ident]struct{})
	for _, expr := range values {
		p.collectGlobalDependencies(expr, deps)
	}
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
	case *AddressExpr:
		p.collectGlobalDependencies(n.Target, deps)
	case *TypeAssertExpr:
		p.collectGlobalDependencies(n.X, deps)
	case *ReceiveExpr:
		p.collectGlobalDependencies(n.Channel, deps)
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
	for i := 0; i < len(b.Children); i++ {
		child := b.Children[i]
		if child == nil {
			semCtx.AddErrorf("block contains nil statement")
			hasError = true
			continue
		}
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
		if child == nil {
			continue
		}
		optimized := child.Optimize(ctx)
		if optimized == nil {
			continue
		}

		// 移除定义语句并确保已注册
		if fn, ok := optimized.(*FunctionStmt); ok {
			ctx.root.program.Functions[fn.RegistryName()] = fn
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
	loopCtx := ctx.Child(f)
	var hasError bool

	if f.Init != nil {
		if err := f.Init.Check(loopCtx); err != nil {
			hasError = true
		}
	}

	if f.Cond != nil {
		logCount := loopCtx.LogCount()
		if err := f.Cond.Check(loopCtx); ForwardStructuredError(loopCtx, f.Cond, logCount, err) {
			hasError = true
		} else {
			condType := f.Cond.GetBase().Type
			if condType != "" && !condType.Equals("Bool") {
				err := fmt.Errorf("for循环条件必须是Bool类型, 实际为 %s", condType)
				loopCtx.AddErrorAt(f.Cond, "%s", err.Error())
				hasError = true
			}
		}
	}

	if f.Update != nil {
		if err := f.Update.Check(loopCtx); err != nil {
			hasError = true
		}
	}

	if f.Body == nil {
		err := errors.New("for循环缺少主体")
		loopCtx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		if err := f.Body.Check(loopCtx); err != nil {
			hasError = true
		}
		if _, ok := f.Body.(*BlockStmt); !ok {
			err := errors.New("循环主体不是 block")
			loopCtx.AddErrorf("%s", err.Error())
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
			expectedItems, expectedTuple := expectedReturn.ReadTuple()
			for _, result := range r.Results {
				resultType := result.GetBase().Type
				if resultType.IsTuple() {
					i := len(rTypes)
					if !expectedTuple || i >= len(expectedItems) || !expectedItems[i].IsTuple() {
						err := fmt.Errorf("multiple-value return used in single-value result slot: %s", resultType)
						ctx.AddErrorAt(result, "%s", err.Error())
						return err
					}
				}
				rTypes = append(rTypes, resultType)
			}
			tType = CreateTupleType(rTypes...)
		} else {
			tType = r.Results[0].GetBase().Type
			if tType.IsTuple() && !expectedReturn.IsTuple() {
				err := fmt.Errorf("multiple-value return used in single-value result slot: %s", tType)
				ctx.AddErrorAt(r.Results[0], "%s", err.Error())
				return err
			}
		}

		if !tType.IsAssignableTo(expectedReturn) {
			err := fmt.Errorf("return type mismatch: return %s != function %s", expectedReturn, tType)
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

// GoStmt 表示异步启动任务语句。
type GoStmt struct {
	BaseNode
	Call Expr
}

func (g *GoStmt) GetBase() *BaseNode { return &g.BaseNode }
func (g *GoStmt) stmtNode()          {}

func (g *GoStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(g)
	g.Type = "Void"
	if g.Call == nil {
		err := errors.New("go 语句缺少调用表达式")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	call, ok := g.Call.(*CallExprStmt)
	if !ok {
		err := errors.New("go 语句只支持调用表达式")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	return call.Check(ctx.WithNode(call))
}

func (g *GoStmt) Optimize(ctx *OptimizeContext) Node {
	if g.Call != nil {
		if opt := g.Call.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				g.Call = val
			}
		}
	}
	return g
}

// SendStmt 表示 channel 发送语句 (ch <- value)。
type SendStmt struct {
	BaseNode
	Channel Expr `json:"channel"`
	Value   Expr `json:"value"`
}

func (s *SendStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *SendStmt) stmtNode()          {}

func (s *SendStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(s)
	s.Type = TypeVoid
	var hasError bool
	if s.Channel == nil {
		ctx.AddErrorf("channel send 缺少 channel 表达式")
		hasError = true
	} else if err := s.Channel.Check(ctx.WithNode(s.Channel)); err != nil {
		hasError = true
	}
	if s.Value == nil {
		ctx.AddErrorf("channel send 缺少发送值")
		hasError = true
	} else if err := s.Value.Check(ctx.WithNode(s.Value)); err != nil {
		hasError = true
	}
	if hasError {
		return errors.New("send statement validation failed")
	}
	chType := s.Channel.GetBase().Type
	if chType.IsAny() {
		return nil
	}
	if !chType.IsChan() || chType.IsRecvChan() {
		err := fmt.Errorf("cannot send to non-send channel type: %s", chType)
		ctx.AddErrorAt(s.Channel, "%s", err.Error())
		return err
	}
	elem, ok := chType.ReadChanElemType()
	if !ok {
		err := fmt.Errorf("invalid channel type: %s", chType)
		ctx.AddErrorAt(s.Channel, "%s", err.Error())
		return err
	}
	valueType := s.Value.GetBase().Type
	if elem.IsVoid() {
		return nil
	}
	if !valueType.IsAssignableTo(elem) {
		err := fmt.Errorf("channel send type mismatch: cannot send %s to %s", valueType, chType)
		ctx.AddErrorAt(s.Value, "%s", err.Error())
		return err
	}
	return nil
}

func (s *SendStmt) Optimize(ctx *OptimizeContext) Node {
	if s.Channel != nil {
		if opt := s.Channel.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				s.Channel = val
			}
		}
	}
	if s.Value != nil {
		if opt := s.Value.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				s.Value = val
			}
		}
	}
	return s
}

// SelectStmt 表示 channel select 语句。
type SelectStmt struct {
	BaseNode
	Cases []SelectCase `json:"cases"`
}

type SelectCase struct {
	BaseNode
	Comm Stmt   `json:"comm,omitempty"`
	Body []Stmt `json:"body,omitempty"`
}

func (c *SelectCase) GetBase() *BaseNode             { return &c.BaseNode }
func (c *SelectCase) Check(_ *SemanticContext) error { return nil }
func (c *SelectCase) Optimize(_ *OptimizeContext) Node {
	return c
}

func (s *SelectStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *SelectStmt) stmtNode()          {}

func (s *SelectStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(s)
	s.Type = TypeVoid
	var hasError bool
	seenDefault := false
	for i := range s.Cases {
		c := &s.Cases[i]
		caseCtx := ctx.Child(c)
		if c.Comm == nil {
			if seenDefault {
				caseCtx.AddErrorf("select has multiple default cases")
				hasError = true
			}
			seenDefault = true
		} else {
			switch comm := c.Comm.(type) {
			case *SendStmt:
				if err := comm.Check(caseCtx); err != nil {
					hasError = true
				}
			case *AssignmentStmt:
				if _, ok := comm.Value.(*ReceiveExpr); !ok {
					caseCtx.AddErrorf("select receive assignment must use <-channel")
					hasError = true
				} else if err := comm.Check(caseCtx); err != nil {
					hasError = true
				}
			case *MultiAssignmentStmt:
				if len(comm.Values) != 1 {
					caseCtx.AddErrorf("select receive assignment must use one receive expression")
					hasError = true
				} else if _, ok := comm.Values[0].(*ReceiveExpr); !ok {
					caseCtx.AddErrorf("select receive assignment must use <-channel")
					hasError = true
				} else if err := comm.Check(caseCtx); err != nil {
					hasError = true
				}
			case *ExpressionStmt:
				if _, ok := comm.X.(*ReceiveExpr); !ok {
					caseCtx.AddErrorf("select expression case must be <-channel")
					hasError = true
				} else if err := comm.Check(caseCtx); err != nil {
					hasError = true
				}
			default:
				caseCtx.AddErrorf("unsupported select case statement %T", comm)
				hasError = true
			}
		}
		body := &BlockStmt{Children: c.Body, Inner: true}
		if err := body.Check(caseCtx); err != nil {
			hasError = true
		}
	}
	if hasError {
		return errors.New("select statement validation failed")
	}
	return nil
}

func (s *SelectStmt) Optimize(ctx *OptimizeContext) Node {
	for i := range s.Cases {
		if s.Cases[i].Comm != nil {
			if opt := s.Cases[i].Comm.Optimize(ctx); opt != nil {
				if stmt, ok := opt.(Stmt); ok {
					s.Cases[i].Comm = stmt
				}
			}
		}
		for j, stmt := range s.Cases[i].Body {
			if stmt == nil {
				continue
			}
			if opt := stmt.Optimize(ctx); opt != nil {
				if st, ok := opt.(Stmt); ok {
					s.Cases[i].Body[j] = st
				}
			}
		}
	}
	return s
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
	rangeCtx := ctx.Child(r)

	if r.X == nil {
		err := errors.New("range 语句缺少对象")
		rangeCtx.AddErrorf("%s", err.Error())
		hasError = true
	} else {
		if err := r.X.Check(rangeCtx); err != nil {
			hasError = true
		} else {
			objType := r.X.GetBase().Type
			if !objType.IsArray() && !objType.IsMap() && !objType.IsChan() && !objType.IsAny() {
				err := fmt.Errorf("range 语句不支持类型 %s", objType)
				rangeCtx.AddErrorf("%s", err.Error())
				hasError = true
			}
		}
	}

	objType := GoMiniType("Any")
	if r.X != nil {
		objType = r.X.GetBase().Type
	}

	keyType := GoMiniType("Int64")
	valueType := GoMiniType("Any")
	if objType.IsMap() {
		keyType, valueType, _ = objType.GetMapKeyValueTypes()
	} else if objType.IsArray() {
		valueType, _ = objType.ReadArrayItemType()
	} else if objType.IsChan() {
		if objType.IsSendChan() {
			rangeCtx.AddErrorf("range cannot receive from send-only channel type %s", objType)
			hasError = true
		}
		keyType, _ = objType.ReadChanElemType()
		valueType = TypeVoid
		if r.Value != "" && r.Value != "_" {
			rangeCtx.AddErrorf("range over channel permits only one iteration variable")
			hasError = true
		}
	}

	r.Key = r.Key.Resolve(ctx.ValidContext)
	r.Value = r.Value.Resolve(ctx.ValidContext)
	if r.Define && r.Key != "" && r.Key != "_" && r.Value != "" && r.Value != "_" && r.Key == r.Value {
		rangeCtx.AddErrorf("%s repeated on left side of :=", r.Key)
		hasError = true
	}

	if r.Key != "" && r.Key != "_" {
		if r.Define {
			rangeCtx.AddVariable(r.Key, keyType)
		} else {
			existingType, ok := rangeCtx.GetVariable(r.Key)
			if !ok {
				err := fmt.Errorf("undefined identifier in assignment: %s", r.Key)
				rangeCtx.AddErrorf("%s", err.Error())
				hasError = true
			} else if !keyType.IsAssignableTo(existingType) {
				err := fmt.Errorf("type mismatch: cannot assign %s to %s (%s)", keyType, r.Key, existingType)
				rangeCtx.AddErrorf("%s", err.Error())
				hasError = true
			}
		}
	}

	if r.Value != "" && r.Value != "_" {
		if r.Define {
			rangeCtx.AddVariable(r.Value, valueType)
		} else {
			existingType, ok := rangeCtx.GetVariable(r.Value)
			if !ok {
				err := fmt.Errorf("undefined identifier in assignment: %s", r.Value)
				rangeCtx.AddErrorf("%s", err.Error())
				hasError = true
			} else if !valueType.IsAssignableTo(existingType) {
				err := fmt.Errorf("type mismatch: cannot assign %s to %s (%s)", valueType, r.Value, existingType)
				rangeCtx.AddErrorf("%s", err.Error())
				hasError = true
			}
		}
	}

	if r.Body == nil {
		err := errors.New("range 语句缺少主体")
		rangeCtx.AddErrorf("%s", err.Error())
		hasError = true
	} else if err := r.Body.Check(rangeCtx); err != nil {
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
		if child == nil {
			ctx.AddErrorf("switch body contains nil statement")
			hasError = true
			continue
		}
		clause, ok := child.(*CaseClause)
		if !ok {
			err := fmt.Errorf("switch 语句只能包含 CaseClause, 得到 %T", child)
			ctx.AddErrorf("%s", err.Error())
			hasError = true
			continue
		}
		if s.IsType {
			assignName := Ident("")
			if assign, ok := s.Assign.(*AssignmentStmt); ok {
				if id, ok := assign.LHS.(*IdentifierExpr); ok {
					assignName = id.Name
				}
			}
			// 在 Type Switch 中，Case 列表中的表达式应该代表类型
			caseType := TypeAny
			for _, expr := range clause.List {
				if expr == nil {
					continue
				}
				// 标记为类型校验
				expr.GetBase().IsType = true
				logCount := inner.LogCount()
				if err := expr.Check(inner); ForwardStructuredError(inner, expr, logCount, err) {
					hasError = true
					continue
				}
				if len(clause.List) == 1 {
					caseType = expr.GetBase().Type
				}
			}
			// 校验 Case 的 Body
			caseCtx := inner
			if assignName != "" {
				caseCtx = inner.Child(clause)
				if caseType == "nil" {
					caseType = TypeAny
				}
				caseCtx.AddVariable(assignName, caseType)
			}
			for _, stmt := range clause.Body {
				if stmt == nil {
					ctx.AddErrorf("case body contains nil statement")
					hasError = true
					continue
				}
				logCount := caseCtx.LogCount()
				if err := stmt.Check(caseCtx); ForwardStructuredError(caseCtx, stmt, logCount, err) {
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
		if expr == nil {
			ctx.AddErrorf("case list contains nil expression")
			hasError = true
			continue
		}
		if err := expr.Check(ctx); err != nil {
			hasError = true
		} else {
			if !typespec.EqualityComparable(typespec.Type(tagType), typespec.Type(expr.GetBase().Type)) {
				err := fmt.Errorf("case type mismatch: cannot compare %s with %s", tagType, expr.GetBase().Type)
				ctx.AddErrorAt(expr, "%s", err.Error())
				hasError = true
			}
			c.List[i] = expr.Optimize(NewOptimizeContext(ctx.ValidContext)).(Expr)
		}
	}
	for i, stmt := range c.Body {
		if stmt == nil {
			ctx.AddErrorf("case body contains nil statement")
			hasError = true
			continue
		}
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

func (f *FunctionStmt) RegistryName() Ident {
	if f == nil {
		return ""
	}
	if f.ReceiverType == "" {
		return f.Name
	}
	return Ident(string(f.ReceiverType) + "." + string(f.Name))
}

// PreRegister 预注册函数签名 (用于支持相互递归)
func (f *FunctionStmt) PreRegister(ctx *ValidContext) (*ValidStruct, bool) {
	var structType *ValidStruct
	fnName := f.Name
	isMethod := false

	// 首先检查 ReceiverType 字段
	if f.ReceiverType != "" {
		receiver := GoMiniType(f.ReceiverType).Resolve(ctx)
		if elem, ok := receiver.GetPtrElementType(); ok {
			receiver = elem
		}
		f.ReceiverType = Ident(receiver)
		st, ok := ctx.GetStruct(Ident(receiver))
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
	if ctx.ContainsHostOpaqueValue(f.FunctionType.Return) {
		ctx.AddErrorf("函数 %s 返回值不能使用 opaque host type: %s", f.Name, f.FunctionType.Return)
		return nil, false
	}

	for i, param := range f.Params {
		f.Params[i].Type = param.Type.Resolve(ctx)
		if !f.Params[i].Type.Valid(ctx) {
			return nil, false
		}
		if ctx.ContainsHostOpaqueValue(f.Params[i].Type) {
			ctx.AddErrorf("函数 %s 参数 %s 不能使用 opaque host type: %s", f.Name, param.Name, f.Params[i].Type)
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
		if !isMethod {
			ctx.root.vars[fnName] = f.FunctionType.MiniType()
		}
		return structType, true
	}

	structType.Methods[fnName] = sig
	if !isMethod {
		ctx.root.vars[fnName] = f.FunctionType.MiniType()
	}

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
	seenParams := make(map[Ident]struct{}, len(f.Params))
	for _, param := range f.Params {
		if param.Name == "" || !param.Name.Valid(funcCtx.ValidContext) {
			err := fmt.Errorf("invalid param name: %s", param.Name)
			funcCtx.AddErrorf("%s", err.Error())
			hasError = true
		}
		if param.Name != "" && param.Name != "_" {
			if _, exists := seenParams[param.Name]; exists {
				err := fmt.Errorf("parameter redeclared: %s", param.Name)
				funcCtx.AddErrorf("%s", err.Error())
				hasError = true
			}
			seenParams[param.Name] = struct{}{}
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
		if param.Name != "" && param.Name != "_" {
			bodyCtx.AddVariable(param.Name, param.Type)
		}
	}

	// 3. 注册到程序中
	f.Type = "Void"
	ctx.root.program.Functions[f.RegistryName()] = f

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
				err := fmt.Errorf("function %s is missing a return statement", f.Name)
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
	Kind   AssignKind `json:"kind"`
	LHS    []Expr     `json:"lhs"`
	Values []Expr     `json:"values"`
}

func (m *MultiAssignmentStmt) GetBase() *BaseNode { return &m.BaseNode }
func (m *MultiAssignmentStmt) stmtNode()          {}

func (m *MultiAssignmentStmt) Check(ctx *SemanticContext) error {
	if m.Kind != AssignSet && m.Kind != AssignDefine {
		err := errors.New("multi assignment missing assignment kind")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	m.Type = "Void"
	if len(m.LHS) == 0 {
		err := errors.New("multi assignment missing LHS")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if len(m.Values) == 0 {
		err := errors.New("multi assignment missing values")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
	if len(m.Values) != len(m.LHS) && !(len(m.Values) == 1 && len(m.LHS) > 1) {
		err := fmt.Errorf("assignment count mismatch: %d names = %d values", len(m.LHS), len(m.Values))
		ctx.AddErrorf("%s", err.Error())
		return err
	}

	var hasError bool
	if m.Kind == AssignDefine {
		seen := make(map[Ident]struct{}, len(m.LHS))
		for _, lhs := range m.LHS {
			if lhs == nil {
				continue
			}
			ident, ok := lhs.(*IdentifierExpr)
			if !ok || ident == nil {
				ctx.AddErrorAt(lhs, "non-name on left side of :=")
				hasError = true
				continue
			}
			ident.Name = ident.Name.Resolve(ctx.ValidContext)
			if ident.Name == "_" {
				continue
			}
			if _, exists := seen[ident.Name]; exists {
				ctx.AddErrorAt(ident, "%s repeated on left side of :=", ident.Name)
				hasError = true
				continue
			}
			seen[ident.Name] = struct{}{}
		}
	}

	perBinding := len(m.Values) == len(m.LHS)
	valueTypes := make([]GoMiniType, len(m.Values))
	for i, value := range m.Values {
		if value == nil {
			ctx.AddErrorf("multi assignment has nil value")
			hasError = true
			continue
		}
		logCount := ctx.LogCount()
		if err := value.Check(ctx); ForwardStructuredError(ctx, value, logCount, err) {
			hasError = true
			continue
		}
		typ := value.GetBase().Type
		if typ.IsEmpty() || typ.IsVoid() {
			ctx.AddErrorf("assignment value has invalid type: %s", typ)
			hasError = true
			continue
		}
		valueTypes[i] = typ
	}
	if hasError {
		return errors.New("multi assignment validation failed")
	}

	var elementTypes []GoMiniType
	if perBinding {
		elementTypes = valueTypes
	} else {
		switch valType := valueTypes[0]; {
		case valType.IsTuple():
			elementTypes, _ = valType.ReadTuple()
		case valType.IsArray():
			itemType, _ := valType.ReadArrayItemType()
			for i := 0; i < len(m.LHS); i++ {
				elementTypes = append(elementTypes, itemType)
			}
		case valType.IsAny():
			for i := 0; i < len(m.LHS); i++ {
				elementTypes = append(elementTypes, TypeAny)
			}
		default:
			err := fmt.Errorf("cannot destructure non-composite type: %s", valType)
			ctx.AddErrorf("%s", err.Error())
			return err
		}
		if len(m.LHS) != len(elementTypes) {
			err := fmt.Errorf("assignment count mismatch: %d = %d", len(m.LHS), len(elementTypes))
			ctx.AddErrorf("%s", err.Error())
			return err
		}
	}

	type pendingVar struct {
		name Ident
		typ  GoMiniType
	}
	pendingNew := make([]pendingVar, 0, len(m.LHS))
	newCount := 0
	for i, lhs := range m.LHS {
		targetType := elementTypes[i]
		value := Expr(nil)
		if perBinding {
			value = m.Values[i]
		}
		if lhs == nil {
			if perBinding && targetType.IsTuple() {
				ctx.AddErrorf("multiple-value initializer used in single-value assignment slot: %s", targetType)
				hasError = true
			}
			continue
		}

		if ident, ok := lhs.(*IdentifierExpr); ok {
			ident.Name = ident.Name.Resolve(ctx.ValidContext)
			if ident.Name == "_" {
				if perBinding && targetType.IsTuple() {
					ctx.AddErrorAt(value, "multiple-value initializer used in single-value assignment slot: %s", targetType)
					hasError = true
				}
				ident.Type = targetType
				continue
			}
			if m.Kind == AssignDefine {
				if vType, sameScope := ctx.vars[ident.Name]; sameScope {
					if perBinding && targetType.IsTuple() && !vType.IsTuple() {
						ctx.AddErrorAt(value, "multiple-value initializer used in single-value assignment slot: %s", targetType)
						hasError = true
						continue
					}
					if !targetType.IsAssignableTo(vType) {
						err := fmt.Errorf("type mismatch at index %d: cannot assign %s to %s (%s)", i, targetType, ident.Name, vType)
						ctx.AddErrorf("%s", err.Error())
						hasError = true
					}
					ident.Type = targetType
					continue
				}
				if perBinding && targetType.IsTuple() {
					ctx.AddErrorAt(value, "multiple-value initializer used in single-value assignment slot: %s", targetType)
					hasError = true
					continue
				}
				if !targetType.IsVoid() {
					pendingNew = append(pendingNew, pendingVar{name: ident.Name, typ: targetType})
					ident.Type = targetType
					newCount++
				}
				continue
			}

			vType, exists := ctx.GetVariable(ident.Name)
			if !exists {
				if _, ok := ctx.GetConstant(ident.Name); ok {
					err := fmt.Errorf("cannot assign to constant %s", ident.Name)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
					continue
				}
				err := fmt.Errorf("undefined identifier in assignment: %s", ident.Name)
				ctx.AddErrorf("%s", err.Error())
				hasError = true
			} else {
				if ctx.IsReadOnlyVariable(ident.Name) {
					err := fmt.Errorf("cannot assign to read-only external symbol %s", ident.Name)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
					continue
				}
				if perBinding && targetType.IsTuple() && !vType.IsTuple() {
					ctx.AddErrorAt(value, "multiple-value initializer used in single-value assignment slot: %s", targetType)
					hasError = true
					continue
				}
				if !targetType.IsAssignableTo(vType) {
					err := fmt.Errorf("type mismatch at index %d: cannot assign %s to %s (%s)", i, targetType, ident.Name, vType)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
				}
				ident.Type = targetType
			}
		} else {
			if err := lhs.Check(ctx); err != nil {
				hasError = true
			} else {
				if member, ok := lhs.(*MemberExpr); ok && member.ResolvedPackageMember && ctx.IsReadOnlyVariable(member.ResolvedPackageName) {
					err := fmt.Errorf("cannot assign to read-only external symbol %s", member.ResolvedPackageName)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
					continue
				}
				lhsType := lhs.GetBase().Type
				if perBinding && targetType.IsTuple() && !lhsType.IsTuple() {
					ctx.AddErrorAt(value, "multiple-value initializer used in single-value assignment slot: %s", targetType)
					hasError = true
					continue
				}
				if !targetType.IsAssignableTo(lhsType) {
					err := fmt.Errorf("assignment type mismatch at index %d: LHS is %s, RHS is %s", i, lhsType, targetType)
					ctx.AddErrorf("%s", err.Error())
					hasError = true
				}
			}
		}
	}
	if m.Kind == AssignDefine && newCount == 0 {
		ctx.AddErrorf("no new variables on left side of :=")
		hasError = true
	}

	if hasError {
		return errors.New("multi assignment validation failed")
	}
	for _, item := range pendingNew {
		ctx.AddVariable(item.name, item.typ)
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
	for i, value := range m.Values {
		if value == nil {
			continue
		}
		if opt := value.Optimize(ctx); opt != nil {
			if val, ok := opt.(Expr); ok {
				m.Values[i] = val
			}
		}
	}
	return m
}

// VarBinding describes one name in a var declaration.
type VarBinding struct {
	Name     Ident      `json:"name"`
	Kind     GoMiniType `json:"kind,omitempty"`
	Inferred bool       `json:"inferred,omitempty"`
}

type resolvedVarBinding struct {
	index    int
	name     Ident
	kind     GoMiniType
	inferred bool
}

// GenDeclStmt 变量声明
type GenDeclStmt struct {
	BaseNode
	Bindings []VarBinding `json:"bindings"`
	Values   []Expr       `json:"values,omitempty"`
}

func (g *GenDeclStmt) GetBase() *BaseNode { return &g.BaseNode }
func (g *GenDeclStmt) stmtNode()          {}
func (g *GenDeclStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(g)
	g.Type = "Void"
	infos, ok := g.resolveBindings(ctx, false)
	if !ok {
		return errors.New("variable declaration validation failed")
	}
	finalTypes, ok := g.resolveValueTypes(ctx, infos)
	if !ok {
		return errors.New("variable declaration validation failed")
	}
	for i, info := range infos {
		if info.name == "_" {
			continue
		}
		ctx.AddVariable(info.name, finalTypes[i])
	}
	return nil
}

func (g *GenDeclStmt) Optimize(ctx *OptimizeContext) Node {
	for i, value := range g.Values {
		if value == nil {
			continue
		}
		if opt := value.Optimize(ctx); opt != nil {
			if expr, ok := opt.(Expr); ok {
				g.Values[i] = expr
			}
		}
	}
	return g
}

func (g *GenDeclStmt) resolveBindings(ctx *SemanticContext, allowExisting bool) ([]resolvedVarBinding, bool) {
	if len(g.Bindings) == 0 {
		ctx.AddErrorf("variable declaration missing bindings")
		return nil, false
	}

	infos := make([]resolvedVarBinding, 0, len(g.Bindings))
	seen := make(map[Ident]struct{}, len(g.Bindings))
	ok := true
	for i := range g.Bindings {
		binding := &g.Bindings[i]
		binding.Name = binding.Name.Resolve(ctx.ValidContext)
		name := binding.Name

		if name == "" || !name.Valid(ctx.ValidContext) {
			ctx.AddErrorf("invalid identifier: %s", name)
			ok = false
			continue
		}
		if name != "_" {
			if _, exists := seen[name]; exists {
				ctx.AddErrorf("variable redeclared in this block: %s", name)
				ok = false
				continue
			}
			seen[name] = struct{}{}
			if !allowExisting && ctx.IsLocalVariable(name) {
				ctx.AddErrorf("variable redeclared in this block: %s", name)
				ok = false
				continue
			}
		}

		kind := binding.Kind.Resolve(ctx.ValidContext)
		if binding.Inferred {
			binding.Kind = kind
			infos = append(infos, resolvedVarBinding{index: i, name: name, kind: kind, inferred: true})
			continue
		}
		if kind.IsEmpty() {
			ctx.AddErrorf("variable %s declaration missing type", name)
			ok = false
			continue
		}
		if kind.IsVoid() {
			ctx.AddErrorf("不能声明 void 类型的变量: %s", name)
			ok = false
			continue
		}
		if !kind.Valid(ctx.ValidContext) {
			ctx.AddErrorf("invalid type: %s", kind)
			ok = false
			continue
		}
		if ctx.ContainsHostOpaqueValue(kind) {
			ctx.AddErrorf("变量 %s 不能声明为 opaque host value 类型: %s", name, kind)
			ok = false
			continue
		}
		binding.Kind = kind
		infos = append(infos, resolvedVarBinding{index: i, name: name, kind: kind})
	}
	return infos, ok
}

func (g *GenDeclStmt) resolveValueTypes(ctx *SemanticContext, infos []resolvedVarBinding) ([]GoMiniType, bool) {
	finalTypes := make([]GoMiniType, len(infos))
	if len(g.Values) == 0 {
		ok := true
		for i, info := range infos {
			if info.inferred {
				ctx.AddErrorf("cannot infer type for %s without initializer", info.name)
				ok = false
				continue
			}
			finalTypes[i] = info.kind
		}
		return finalTypes, ok
	}

	if len(g.Values) != len(infos) && !(len(g.Values) == 1 && len(infos) > 1) {
		ctx.AddErrorf("variable declaration count mismatch: %d names = %d values", len(infos), len(g.Values))
		return nil, false
	}

	valueTypes := make([]GoMiniType, len(g.Values))
	ok := true
	for i, value := range g.Values {
		if value == nil {
			ctx.AddErrorf("variable declaration has nil initializer")
			ok = false
			continue
		}
		if len(g.Values) == len(infos) && !infos[i].inferred {
			if sub, isComposite := value.(*CompositeExpr); isComposite && sub.Kind == "" {
				sub.BaseNode.Type = infos[i].kind
			}
		}
		logCount := ctx.LogCount()
		if err := value.Check(ctx); ForwardStructuredError(ctx, value, logCount, err) {
			ok = false
			continue
		}
		typ := value.GetBase().Type
		if typ.IsEmpty() || typ.IsVoid() {
			ctx.AddErrorf("initializer has invalid type: %s", typ)
			ok = false
			continue
		}
		valueTypes[i] = typ
	}
	if !ok {
		return nil, false
	}

	if len(g.Values) == len(infos) {
		copy(finalTypes, valueTypes)
	} else {
		switch typ := valueTypes[0]; {
		case typ.IsTuple():
			items, _ := typ.ReadTuple()
			if len(items) != len(infos) {
				ctx.AddErrorf("tuple declaration count mismatch: %d names = %d values", len(infos), len(items))
				return nil, false
			}
			copy(finalTypes, items)
		case typ.IsArray():
			itemType, _ := typ.ReadArrayItemType()
			for i := range finalTypes {
				finalTypes[i] = itemType
			}
		case typ.IsAny():
			for i := range finalTypes {
				finalTypes[i] = TypeAny
			}
		default:
			ctx.AddErrorf("cannot destructure non-composite type: %s", typ)
			return nil, false
		}
	}

	for i, info := range infos {
		valueType := finalTypes[i]
		if info.inferred {
			if len(g.Values) == len(infos) && valueType.IsTuple() {
				ctx.AddErrorAt(g.Values[i], "multiple-value initializer used in single-value declaration slot: %s", valueType)
				ok = false
				continue
			}
			g.Bindings[info.index].Kind = valueType
			continue
		}
		if len(g.Values) == len(infos) && valueType.IsTuple() && !info.kind.IsTuple() {
			ctx.AddErrorAt(g.Values[i], "multiple-value initializer used in single-value declaration slot: %s", valueType)
			ok = false
			continue
		}
		if !valueType.IsAssignableTo(info.kind) {
			ctx.AddErrorf("type mismatch: cannot assign %s to %s (%s)", valueType, info.name, info.kind)
			ok = false
			continue
		}
		finalTypes[i] = info.kind
	}
	return finalTypes, ok
}

// AssignmentStmt 表示赋值语句
type AssignmentStmt struct {
	BaseNode
	Kind  AssignKind `json:"kind"`
	LHS   Expr       `json:"lhs"`
	Value Expr       `json:"value"`
}

func (a *AssignmentStmt) GetBase() *BaseNode { return &a.BaseNode }
func (a *AssignmentStmt) stmtNode()          {}

// DeferStmt, RangeStmt, SwitchStmt are defined earlier
func (a *AssignmentStmt) Check(ctx *SemanticContext) error {
	ctx = ctx.WithNode(a)
	if a.Kind != AssignSet && a.Kind != AssignDefine {
		err := errors.New("assignment missing assignment kind")
		ctx.AddErrorf("%s", err.Error())
		return err
	}
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

	// Identifier assignment participates in strict declaration/assignment rules.
	if ident, ok := a.LHS.(*IdentifierExpr); ok {
		ident.Name = ident.Name.Resolve(ctx.ValidContext)
		if ident.Name == "_" {
			if err := a.Value.Check(ctx); err != nil {
				return err
			}
			miniType := a.Value.GetBase().Type
			if miniType.IsTuple() {
				err := fmt.Errorf("multiple-value initializer used in single-value assignment slot: %s", miniType)
				ctx.AddErrorAt(a.Value, "%s", err.Error())
				return err
			}
			if a.Kind == AssignDefine {
				err := errors.New("no new variables on left side of :=")
				ctx.AddErrorf("%s", err.Error())
				return err
			}
			ident.Type = miniType
			return nil
		}

		if a.Kind == AssignDefine {
			if ctx.parent == nil && ctx.root.Package != "" && ctx.root.Package != "main" && !strings.Contains(string(ident.Name), ".") {
				ident.Name = Ident(fmt.Sprintf("%s.%s", ctx.root.Package, ident.Name))
			}
			if vType, sameScope := ctx.vars[ident.Name]; sameScope {
				if sub, ok := a.Value.(*CompositeExpr); ok && sub.Kind == "" {
					sub.BaseNode.Type = vType
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
				if miniType.IsTuple() && !vType.IsTuple() {
					err := fmt.Errorf("multiple-value initializer used in single-value assignment slot: %s", miniType)
					ctx.AddErrorAt(a.Value, "%s", err.Error())
					return err
				}
				if !miniType.IsAssignableTo(vType) {
					err := fmt.Errorf("type mismatch: cannot assign %s to %s (%s)", miniType, ident.Name, vType)
					ctx.AddErrorAt(a.Value, "%s", err.Error())
					return err
				}
				ctx.AddErrorf("no new variables on left side of :=")
				return errors.New("assignment validation failed")
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
			if miniType.IsTuple() {
				err := fmt.Errorf("multiple-value initializer used in single-value assignment slot: %s", miniType)
				ctx.AddErrorAt(a.Value, "%s", err.Error())
				return err
			}
			ctx.AddVariable(ident.Name, miniType)
			ident.Type = miniType
			return nil
		}

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
			if ctx.IsReadOnlyVariable(ident.Name) {
				err := fmt.Errorf("cannot assign to read-only external symbol %s", ident.Name)
				ctx.AddErrorAt(a.LHS, "%s", err.Error())
				return err
			}
			if miniType.IsTuple() && !vType.IsTuple() {
				err := fmt.Errorf("multiple-value initializer used in single-value assignment slot: %s", miniType)
				ctx.AddErrorAt(a.Value, "%s", err.Error())
				return err
			}
			if !miniType.IsAssignableTo(vType) {
				err := fmt.Errorf("type mismatch: cannot assign %s to %s (%s)", miniType, ident.Name, vType)
				ctx.AddErrorAt(a.Value, "%s", err.Error())
				return err
			}
			// Update the identifier's own type so subsequent uses in the AST might benefit,
			// though typical Check flows rely on GetVariable.
			ident.Type = miniType
			return nil
		}

		if _, ok := ctx.GetConstant(ident.Name); ok {
			err := fmt.Errorf("cannot assign to constant %s", ident.Name)
			ctx.AddErrorAt(a.LHS, "%s", err.Error())
			return err
		}
		err := fmt.Errorf("undefined identifier in assignment: %s", ident.Name)
		ctx.AddErrorAt(a.LHS, "%s", err.Error())
		return err
	}

	// 对于其他复杂的 LHS (IndexExpr, MemberExpr)，直接进行类型检查
	if a.Kind == AssignDefine {
		err := errors.New("non-name on left side of :=")
		ctx.AddErrorAt(a.LHS, "%s", err.Error())
		return err
	}
	if err := a.LHS.Check(ctx); err != nil {
		return err
	}
	if member, ok := a.LHS.(*MemberExpr); ok && member.ResolvedPackageMember && ctx.IsReadOnlyVariable(member.ResolvedPackageName) {
		err := fmt.Errorf("cannot assign to read-only external symbol %s", member.ResolvedPackageName)
		ctx.AddErrorAt(a.LHS, "%s", err.Error())
		return err
	}
	if err := a.Value.Check(ctx); err != nil {
		return err
	}

	lhsType := a.LHS.GetBase().Type
	valType := a.Value.GetBase().Type
	if valType.IsTuple() && !lhsType.IsTuple() {
		err := fmt.Errorf("multiple-value initializer used in single-value assignment slot: %s", valType)
		ctx.AddErrorAt(a.Value, "%s", err.Error())
		return err
	}
	if !valType.IsAssignableTo(lhsType) {
		err := fmt.Errorf("assignment type mismatch: lhs %s, rhs %s", lhsType, valType)
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
			inner.AddVariable(t.Catch.VarName, TypeError)
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
	Name          Ident                `json:"name"`
	QualifiedName Ident                `json:"qualified_name,omitempty"`
	Fields        map[Ident]GoMiniType `json:"fields"`
	FieldNames    []Ident              `json:"field_names,omitempty"`
	FieldLocs     map[Ident]*Position  `json:"field_locs,omitempty"`
	FieldTags     map[Ident]string     `json:"field_tags,omitempty"`
	Doc           string               `json:"doc,omitempty"`
}

// PreRegister 预注册结构体 (用于支持相互引用)
func (s *StructStmt) PreRegister(ctx *ValidContext) bool {
	if !s.Name.Valid(ctx) {
		return false
	}
	s.QualifiedName = ctx.QualifiedTypeName(s.Name)
	registryName := s.QualifiedName
	if registryName == "" {
		registryName = s.Name
	}

	// 提前注册一个空结构体，以支持自引用或循环引用
	if v, ok := ctx.root.structs[registryName]; ok {
		// 检查是否已经定义了字段 (如果是 PreRegister 占位，Fields 通常为空)
		if len(v.Fields) > 0 || len(v.Methods) > 0 {
			ctx.AddErrorf("struct %s 已被定义", s.Name)
			return false
		}
		ctx.root.structs[s.Name] = v
		return true
	}

	vStru := &ValidStruct{
		Fields:    make(map[Ident]GoMiniType),
		Methods:   make(map[Ident]CallFunctionType),
		Ownership: StructOwnershipVMValue,
	}
	ctx.root.structs[registryName] = vStru
	ctx.root.structs[s.Name] = vStru
	return true
}

func (s *StructStmt) GetBase() *BaseNode { return &s.BaseNode }
func (s *StructStmt) stmtNode()          {}

func (s *StructStmt) Check(ctx *SemanticContext) error {
	if s.QualifiedName == "" {
		s.QualifiedName = ctx.QualifiedTypeName(s.Name)
	}
	registryName := s.QualifiedName
	if registryName == "" {
		registryName = s.Name
	}
	if v, ok := ctx.root.structs[registryName]; ok {
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
		if ctx.ContainsHostOpaqueValue(s.Fields[fieldName]) {
			err := fmt.Errorf("struct %s 字段 %s 不能使用 opaque host value 类型: %s", s.Name, fieldName, s.Fields[fieldName])
			ctx.AddErrorf("%s", err.Error())
			hasError = true
		}
	}

	if err := ctx.AddStructDefine(registryName, s.Fields); err != nil {
		err2 := fmt.Errorf("定义struct失败: %v", err)
		ctx.AddErrorf("%s", err2.Error())
		return err2
	}
	ctx.root.structs[registryName].Defined = true
	ctx.root.structs[s.Name] = ctx.root.structs[registryName]

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
