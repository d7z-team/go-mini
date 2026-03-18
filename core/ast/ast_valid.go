package ast

import (
	"errors"
	"fmt"
)

type Logs struct {
	Path    []string
	Level   string
	Message string
}

type ValidRoot struct {
	logs    []Logs
	structs map[Ident]*ValidStruct
	program *ProgramStmt
	Global  *ValidStruct
	id      uint64
	Path    string // 模块的导入路径
	Package string
	Imports map[string]string
	vars    map[Ident]GoMiniType
	Loader  func(path string) (*ProgramStmt, error)
	Imported map[string]bool
	importStack []string
}

type ValidContext struct {
	root        *ValidRoot
	parent      *ValidContext
	current     Node
	vars        map[Ident]GoMiniType
	closureNode *FuncLitExpr // 当前活动的闭包节点
}

func NewValidator(node *ProgramStmt) (*ValidContext, error) {
	imports := make(map[string]string)
	if node.Imports != nil {
		for _, imp := range node.Imports {
			alias := imp.Alias
			if alias == "" {
				alias = imp.Path
			}
			imports[alias] = imp.Path
		}
	}

	pkgName := node.Package
	if pkgName == "" {
		pkgName = "main"
	}

	v := &ValidContext{
		root: &ValidRoot{
			program: node,
			logs:    make([]Logs, 0),
			structs: make(map[Ident]*ValidStruct),
			Global: &ValidStruct{
				Fields:  make(map[Ident]GoMiniType),
				Methods: make(map[Ident]CallFunctionType),
			},
			Package: pkgName,
			Path:    pkgName, // 默认为包名
			Imports: imports,
			vars:    make(map[Ident]GoMiniType),
			Imported: make(map[string]bool),
			importStack: make([]string, 0),
		},
		parent:  nil,
		current: node,
		vars:    make(map[Ident]GoMiniType),
	}
	// 注入内建 nil
	v.root.vars["nil"] = "Any"
	return v, nil
}

func (c *ValidContext) Child(b Node) *ValidContext {
	if b != nil {
		b.GetBase().EnsureID(c)
	}
	if c.current == b {
		return c
	}
	return &ValidContext{
		root:        c.root,
		parent:      c,
		current:     b,
		vars:        make(map[Ident]GoMiniType),
		closureNode: c.closureNode,
	}
}

type ValidStruct struct {
	Fields  map[Ident]GoMiniType
	Methods map[Ident]CallFunctionType
	Defined bool
}

func (c *ValidContext) NextID() uint64 {
	c.root.id++
	return c.root.id
}

func (c *ValidContext) AddErrorf(message string, args ...interface{}) {
	path := make([]string, 0)
	msg := fmt.Sprintf(message, args...)
	ctx := c
	for ctx != nil && ctx.current != nil {
		base := ctx.current.GetBase()
		path = append([]string{fmt.Sprintf("%s#%s", base.Meta, base.ID)}, path...)
		ctx = ctx.parent
	}
	c.root.logs = append(c.root.logs, Logs{Path: path, Message: msg})
}

func (c *ValidContext) GetStruct(ident Ident) (*ValidStruct, bool) {
	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.root.structs[ident]; ok {
			return miniType, true
		}
		ctx = ctx.parent
	}

	// 在隔离架构下，Array/Map 仅支持基本操作，不再动态生成方法集定义
	// 校验器仅需知道类型存在即可
	if GoMiniType(ident).IsArray() || GoMiniType(ident).IsMap() {
		return &ValidStruct{Fields: make(map[Ident]GoMiniType), Methods: make(map[Ident]CallFunctionType)}, true
	}

	if GoMiniType(ident).IsPtr() {
		elem, _ := GoMiniType(ident).ReadArrayItemType()
		return c.GetStruct(Ident(elem))
	}
	return nil, false
}

func (c *ValidContext) AddVariable(name Ident, oType GoMiniType) {
	c.vars[name] = oType
	if c.parent == nil {
		c.root.vars[name] = oType
	}
}

func (c *ValidContext) UpdateVariable(name Ident, oType GoMiniType) {
	ctx := c
	for ctx != nil {
		if _, ok := ctx.vars[name]; ok {
			ctx.vars[name] = oType
			return
		}
		ctx = ctx.parent
	}
	if _, ok := c.root.vars[name]; ok {
		c.root.vars[name] = oType
	}
}

func (c *ValidContext) CheckScope(targetMeta string) (Node, bool) {
	item := c
	for item != nil {
		if item.current != nil && item.current.GetBase().Meta == targetMeta {
			return item.current, true
		}
		item = item.parent
	}
	return nil, false
}

func (c *ValidContext) CheckAnyScope(targetMetas ...string) (Node, bool) {
	item := c
	for item != nil {
		if item.current != nil {
			meta := item.current.GetBase().Meta
			for _, t := range targetMetas {
				if meta == t {
					return item.current, true
				}
			}
		}
		item = item.parent
	}
	return nil, false
}

func (c *ValidContext) IsLocalVariable(variable Ident) bool {
	_, ok := c.vars[variable]
	return ok
}

func addCapture(f *FuncLitExpr, name string) {
	for _, n := range f.CaptureNames {
		if n == name {
			return
		}
	}
	f.CaptureNames = append(f.CaptureNames, name)
}

func (c *ValidContext) GetVariable(variable Ident) (GoMiniType, bool) {
	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.vars[variable]; ok {
			// 如果找到了变量，检查我们是否跨越了闭包边界
			c2 := c
			for c2 != ctx {
				if c2.closureNode != nil && c2.closureNode != ctx.closureNode {
					addCapture(c2.closureNode, string(variable))
				}
				c2 = c2.parent
			}
			return miniType, true
		}
		if miniType, ok := ctx.root.vars[variable]; ok {
			// 全局变量无需捕获，因为闭包执行时环境能看到全局
			return miniType, true
		}
		ctx = ctx.parent
	}
	return "", false
}

func (c *ValidContext) GetFunction(fc Ident) (*CallFunctionType, bool) {
	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.root.Global.Methods[fc]; ok {
			return &miniType, true
		}
		ctx = ctx.parent
	}
	return nil, false
}

func (c *ValidContext) Logs() []Logs { return c.root.logs }

func (c *ValidContext) AddFuncSpec(name Ident, miniType GoMiniType) error {
	a, b := miniType.ReadFunc()
	if !b {
		return errors.New("invalid function type")
	}
	c.root.Global.Methods[name] = a.ToCallFunctionType()
	return nil
}

func (c *ValidContext) AddStructDefine(name Ident, specs map[Ident]GoMiniType) error {
	var vStru *ValidStruct
	ctx := c
	for ctx != nil {
		if s, ok := ctx.root.structs[name]; ok {
			vStru = s
			break
		}
		ctx = ctx.parent
	}

	if vStru == nil {
		vStru = &ValidStruct{Fields: make(map[Ident]GoMiniType), Methods: make(map[Ident]CallFunctionType)}
		c.root.structs[name] = vStru
	}

	for ident, miniType := range specs {
		if callFunc, b := miniType.ReadCallFunc(); b {
			vStru.Methods[ident] = *callFunc
			ctx.root.Global.Methods[Ident(fmt.Sprintf("__obj__%s__%s", name, ident))] = *callFunc
		} else {
			vStru.Fields[ident] = miniType
		}
	}
	return nil
}

func (c *ValidContext) ConstStore(value string) Ident {
	constID := fmt.Sprintf("__const__%04d", c.NextID())
	for s, s2 := range c.root.program.Constants {
		if s2 == value {
			return Ident(s)
		}
	}
	c.root.program.Constants[constID] = value
	return Ident(constID)
}

func (c *ValidContext) ImportPackage(path string) error {
	if c.root.Loader == nil {
		return nil // 回退到 FFI 行为
	}
	if c.root.Imported[path] {
		return nil
	}

	// 检查当前验证链中是否已存在该路径（循环依赖）
	for _, p := range c.root.importStack {
		if p == path {
			return fmt.Errorf("circular dependency detected in validation: %s", path)
		}
	}

	c.root.Imported[path] = true

	prog, err := c.root.Loader(path)
	if err != nil {
		// 找不到模块时，假设其为 FFI 包
		return nil
	}

	// 在隔离的验证上下文中检查导入的程序，不合并符号
	v, _ := NewValidator(prog)
	v.root.Path = path
	v.SetLoader(c.root.Loader)
	v.root.importStack = append(append([]string(nil), c.root.importStack...), path) // 传递导入栈
	err = prog.Check(NewSemanticContext(v))
	if err != nil {
		return fmt.Errorf("failed to check package %s: %w", path, err)
	}

	return nil
}

func (c *ValidContext) SetLoader(loader func(path string) (*ProgramStmt, error)) {
	c.root.Loader = loader
}

func checkFuncLit(f *FuncLitExpr, ctx *SemanticContext) error {
	funcCtx := NewSemanticContext(ctx.Child(f))
	funcCtx.closureNode = f

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

	// 注意：因为我们要支持闭包，内部函数体需要能够访问到外部的变量，
	// 所以我们这里的 funcCtx.parent 不置为 nil（它默认指向当前的 ctx）。
	// 这使得 bodyCtx 在找不到变量时可以一直向外层作用域查找。

	// 3. 校验函数体
	semBodyCtx := NewSemanticContext(bodyCtx)
	if err := f.Body.Check(semBodyCtx); err != nil {
		return err
	}

	// 4. 返回路径 analysis
	// 对于 FuncLit，如果定义了返回值，同样需要确保有 return 语句
	returnTypes, _ := f.FunctionType.Return.ReadTuple()
	if len(returnTypes) > 0 && !(len(returnTypes) == 1 && returnTypes[0].IsVoid()) {
		analyzer := NewReturnAnalyzer(bodyCtx, f.Return)
		if !analyzer.Analyze(f.Body) {
			analyzer.AddReturnPathErrorsToContext(&funcCtx.ValidContext)
			return fmt.Errorf("匿名函数缺少返回语句")
		}
	}

	return nil
}
