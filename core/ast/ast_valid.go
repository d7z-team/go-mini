package ast

import (
	"errors"
	"fmt"
	"strings"
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
	*ValidStruct
	id              uint64
	Package         string
	Imports         map[string]string
	vars            map[Ident]GoMiniType
	Loader          func(path string) (*ProgramStmt, error)
	Imported        map[string]bool
	PathToPackage   map[string]string
	ImportedFuncs   map[Ident]*FunctionStmt
	ImportedStructs map[Ident]*StructStmt
	ImportedVars    map[Ident]Expr
	ImportedConsts  map[string]string
}

type ValidContext struct {
	root    *ValidRoot
	parent  *ValidContext
	current Node
	vars    map[Ident]GoMiniType
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
			ValidStruct: &ValidStruct{
				Fields:  make(map[Ident]GoMiniType),
				Methods: make(map[Ident]CallFunctionType),
			},
			Package:         pkgName,
			Imports:         imports,
			vars:            make(map[Ident]GoMiniType),
			Imported:        make(map[string]bool),
			PathToPackage:   make(map[string]string),
			ImportedFuncs:   make(map[Ident]*FunctionStmt),
			ImportedStructs: make(map[Ident]*StructStmt),
			ImportedVars:    make(map[Ident]Expr),
			ImportedConsts:  make(map[string]string),
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
		root:    c.root,
		parent:  c,
		current: b,
		vars:    make(map[Ident]GoMiniType),
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
	if miniType, ok := c.root.structs[ident]; ok {
		return miniType, true
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
	if c.parent == nil || strings.Contains(string(name), ".") {
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

func (c *ValidContext) GetVariable(variable Ident) (GoMiniType, bool) {
	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.vars[variable]; ok {
			return miniType, true
		}
		ctx = ctx.parent
	}
	if miniType, ok := c.root.vars[variable]; ok {
		return miniType, true
	}
	return "", false
}

func (c *ValidContext) GetFunction(fc Ident) (*CallFunctionType, bool) {
	if miniType, ok := c.root.Methods[fc]; ok {
		return &miniType, true
	}
	return nil, false
}

func (c *ValidContext) Logs() []Logs { return c.root.logs }

func (c *ValidContext) AddFuncSpec(name Ident, miniType GoMiniType) error {
	a, b := miniType.ReadFunc()
	if !b {
		return errors.New("invalid function type")
	}
	c.root.Methods[name] = a.ToCallFunctionType()
	return nil
}

func (c *ValidContext) AddStructDefine(name Ident, specs map[Ident]GoMiniType) error {
	vStru, exists := c.root.structs[name]
	if !exists {
		vStru = &ValidStruct{Fields: make(map[Ident]GoMiniType), Methods: make(map[Ident]CallFunctionType)}
		c.root.structs[name] = vStru
	}

	for ident, miniType := range specs {
		if callFunc, b := miniType.ReadCallFunc(); b {
			vStru.Methods[ident] = *callFunc
			c.root.Methods[Ident(fmt.Sprintf("__obj__%s__%s", name, ident))] = *callFunc
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
	c.root.Imported[path] = true

	prog, err := c.root.Loader(path)
	if err != nil {
		// 找不到模块时，假设其为 FFI 包
		return nil
	}

	// 将导入包的函数、结构体、变量等合并到当前程序中，并进行语义检查
	// 由于 Check 内部会自动加上 package 前缀，我们直接让 prog 在当前 ctx 下 Check
	
	// 需要注意，prog.Check 会检查 ctx.parent != nil
	// 我们临时将 parent 置为空以通过检查
	oldParent := c.parent
	oldPkg := c.root.Package
	
	c.parent = nil
	c.root.Package = prog.Package
	
	err = prog.Check(NewSemanticContext(c))
	
	c.root.Package = oldPkg
	c.parent = oldParent

	if err != nil {
		return fmt.Errorf("failed to check package %s: %w", path, err)
	}

	// 合并函数、结构体、变量等到当前 program
	for _, v := range prog.Functions {
		c.root.program.Functions[v.Name] = v
	}
	for _, v := range prog.Structs {
		c.root.program.Structs[v.Name] = v
	}
	for k, v := range prog.Variables {
		mangledK := k
		if prog.Package != "" && prog.Package != "main" && !strings.Contains(string(k), ".") {
			mangledK = Ident(fmt.Sprintf("%s.%s", prog.Package, k))
		}
		c.root.program.Variables[mangledK] = v
	}
	for k, v := range prog.Constants {
		mangledK := k
		if prog.Package != "" && prog.Package != "main" && !strings.Contains(k, ".") {
			mangledK = fmt.Sprintf("%s.%s", prog.Package, k)
		}
		c.root.program.Constants[mangledK] = v
	}
	// 不合并 Main (包级别代码/init暂不支持)

	return nil
}

func (c *ValidContext) SetLoader(loader func(path string) (*ProgramStmt, error)) {
	c.root.Loader = loader
}
