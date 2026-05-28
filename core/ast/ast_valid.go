package ast

import (
	"errors"
	"fmt"
	"strings"
)

type Logs struct {
	Node    Node
	Path    []string
	Level   string
	Message string
}

const (
	MaxImportDepth = 100 // 静态验证最大导入深度
)

type ValidRoot struct {
	logs               []Logs
	types              map[Ident]GoMiniType
	structs            map[Ident]*ValidStruct
	interfaces         map[Ident]*InterfaceStmt
	program            *ProgramStmt
	Global             *ValidStruct
	id                 uint64
	Path               string // 模块的导入路径
	Package            string
	Imports            map[string]string
	vars               map[Ident]GoMiniType
	readOnlyVars       map[Ident]bool
	externalTypes      map[Ident]ExternalTypeSpec
	externalConsts     map[string]string
	externalConstTypes map[string]GoMiniType
	ModuleLoader       func(path string) (*ProgramStmt, error)
	Imported           map[string]bool
	Modules            map[string]*ModuleExports
	Discovered         map[Ident]string
	KnownImports       map[string]struct{}
	TemplateBuiltins   map[string]GoMiniType
	importStack        []string
	MaxTypeDepth       int  // 递归类型检查深度限制
	Tolerant           bool // 宽容模式：允许保留不完整 AST 并产出诊断/补全
}

type ValidContext struct {
	root        *ValidRoot
	parent      *ValidContext
	current     Node
	vars        map[Ident]GoMiniType
	closureNode *FuncLitExpr // 当前活动的闭包节点
}

type StructOwnership string

const (
	StructOwnershipVMValue    StructOwnership = "VMValue"
	StructOwnershipHostOpaque StructOwnership = "HostOpaque"
)

type ExternalTypeSpec struct {
	Type      GoMiniType
	Ownership StructOwnership
	ReadOnly  bool
}

func NewValidator(node *ProgramStmt, externalSpecs map[Ident]GoMiniType, externalConsts map[string]string, tolerant bool) (*ValidContext, error) {
	externalTypes := make(map[Ident]ExternalTypeSpec, len(externalSpecs))
	for ident, typ := range externalSpecs {
		externalTypes[ident] = ExternalTypeSpec{Type: typ, Ownership: StructOwnershipVMValue}
	}
	return NewValidatorWithExternalTypesAndConstTypes(node, externalTypes, externalConsts, nil, tolerant)
}

func cloneExternalTypeSpecs(in map[Ident]ExternalTypeSpec) map[Ident]ExternalTypeSpec {
	out := make(map[Ident]ExternalTypeSpec, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneGoMiniTypeMap(in map[string]GoMiniType) map[string]GoMiniType {
	out := make(map[string]GoMiniType, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func NewValidatorWithExternalTypes(node *ProgramStmt, externalTypes map[Ident]ExternalTypeSpec, externalConsts map[string]string, tolerant bool) (*ValidContext, error) {
	return NewValidatorWithExternalTypesAndConstTypes(node, externalTypes, externalConsts, nil, tolerant)
}

func NewValidatorWithExternalTypesAndConstTypes(node *ProgramStmt, externalTypes map[Ident]ExternalTypeSpec, externalConsts map[string]string, externalConstTypes map[string]GoMiniType, tolerant bool) (*ValidContext, error) {
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

	pkgName := "main"
	if node != nil {
		pkgName = node.Package
		if pkgName == "" {
			pkgName = "main"
		}
	}

	v := &ValidContext{
		root: &ValidRoot{
			program:    node,
			logs:       make([]Logs, 0),
			types:      make(map[Ident]GoMiniType),
			structs:    make(map[Ident]*ValidStruct),
			interfaces: make(map[Ident]*InterfaceStmt),
			Global: &ValidStruct{
				Fields:    make(map[Ident]GoMiniType),
				Methods:   make(map[Ident]CallFunctionType),
				Ownership: StructOwnershipVMValue,
			},
			Package:            pkgName,
			Path:               pkgName, // 默认为包名
			Imports:            imports,
			vars:               make(map[Ident]GoMiniType),
			readOnlyVars:       make(map[Ident]bool),
			externalTypes:      cloneExternalTypeSpecs(externalTypes),
			externalConsts:     cloneStringMap(externalConsts),
			externalConstTypes: cloneGoMiniTypeMap(externalConstTypes),
			Imported:           make(map[string]bool),
			Modules:            make(map[string]*ModuleExports),
			Discovered:         make(map[Ident]string),
			KnownImports:       make(map[string]struct{}),
			TemplateBuiltins:   make(map[string]GoMiniType),
			importStack:        make([]string, 0),
			MaxTypeDepth:       256,
			Tolerant:           tolerant,
		},
		parent:  nil,
		current: node,
		vars:    make(map[Ident]GoMiniType),
	}
	if node != nil {
		node.GetBase().Scope = v
	}

	// 注入外部 FFI 常量
	for name, val := range externalConsts {
		if node != nil {
			if node.Constants == nil {
				node.Constants = make(map[string]string)
			}
			if node.ConstantTypes == nil {
				node.ConstantTypes = make(map[string]GoMiniType)
			}
			if _, ok := node.Constants[name]; !ok {
				node.Constants[name] = val
			}
			if typ := externalConstTypes[name]; typ != "" {
				node.ConstantTypes[name] = typ
			}
		}
	}

	// 注入外部 FFI 符号 (如 os.ReadFile)
	for ident, spec := range externalTypes {
		t := spec.Type
		v.root.vars[ident] = t
		if spec.ReadOnly {
			v.root.readOnlyVars[ident] = true
		}
		sIdent := string(ident)
		if idx := strings.Index(sIdent, "."); idx != -1 {
			pkgPath := sIdent[:idx]
			v.root.registerKnownImportPath(pkgPath)

			// 自动推断包名前缀用于补全候选；不要伪装成已导入变量。
			if v.root.Tolerant {
				pkgName := Ident(pkgPath)
				v.root.registerDiscoveredPackage(pkgName, pkgPath)
			}
		}

		if t.IsStruct() {
			fields, _ := t.ReadStructFields()
			ownership := spec.Ownership
			if ownership == "" {
				ownership = StructOwnershipVMValue
			}
			vStru := &ValidStruct{
				Fields:    make(map[Ident]GoMiniType),
				Methods:   make(map[Ident]CallFunctionType),
				Ownership: ownership,
			}
			for fName, fType := range fields {
				if callFunc, ok := fType.ReadCallFunc(); ok {
					vStru.Methods[Ident(fName)] = *callFunc
				} else {
					vStru.Fields[Ident(fName)] = fType
				}
			}
			v.root.structs[ident] = vStru
			continue
		}
		if t.IsInterface() {
			v.root.types[ident] = t
			v.root.interfaces[ident] = &InterfaceStmt{
				Name: ident,
				Type: t,
			}
		}
	}
	// 注入命名接口
	for ident, stmt := range node.Interfaces {
		v.root.interfaces[ident] = stmt
	}

	// 注入命名类型
	for ident, t := range node.Types {
		v.root.types[ident] = t
	}

	// 注入函数
	for ident, fn := range node.Functions {
		v.root.vars[ident] = fn.FunctionType.MiniType()
	}

	// 注入内建 nil
	v.root.vars["nil"] = TypeAny
	v.root.vars["true"] = TypeBool
	v.root.vars["false"] = TypeBool
	return v, nil
}

func (r *ValidRoot) Vars() map[Ident]GoMiniType {
	return r.vars
}

func (r *ValidRoot) registerDiscoveredPackage(alias Ident, path string) {
	if alias == "" || path == "" {
		return
	}
	if _, imported := r.Imports[string(alias)]; imported {
		return
	}
	if _, exists := r.Discovered[alias]; !exists {
		r.Discovered[alias] = path
	}
}

func (r *ValidRoot) registerKnownImportPath(path string) {
	if path == "" {
		return
	}
	r.KnownImports[path] = struct{}{}
	if dotted := strings.ReplaceAll(path, "/", "."); dotted != path {
		r.KnownImports[dotted] = struct{}{}
	}
	if slashed := strings.ReplaceAll(path, ".", "/"); slashed != path {
		r.KnownImports[slashed] = struct{}{}
	}
}

func (r *ValidRoot) HasExternalImportPath(path string) bool {
	if path == "" {
		return false
	}
	if _, ok := r.KnownImports[path]; ok {
		return true
	}
	dotted := strings.ReplaceAll(path, "/", ".")
	if _, ok := r.KnownImports[dotted]; ok {
		return true
	}
	prefixes := []string{path + ".", dotted + "."}
	for name := range r.vars {
		s := string(name)
		for _, prefix := range prefixes {
			if strings.HasPrefix(s, prefix) {
				return true
			}
		}
	}
	return false
}

func (r *ValidRoot) RegisterModuleExports(exports *ModuleExports) {
	if r == nil || exports == nil || exports.Path == "" {
		return
	}
	if r.Modules == nil {
		r.Modules = make(map[string]*ModuleExports)
	}
	r.Modules[exports.Path] = exports
	r.DiscoverModule(exports.Path)
}

func (r *ValidRoot) DiscoverModule(path string) {
	if path == "" {
		return
	}
	r.registerDiscoveredPackage(Ident(path), path)
	if idx := strings.LastIndex(path, "/"); idx != -1 && idx+1 < len(path) {
		r.registerDiscoveredPackage(Ident(path[idx+1:]), path)
	}
}

func (r *ValidRoot) ResolveModule(name Ident) (*ModuleExports, string, bool, bool) {
	if r == nil {
		return nil, "", false, false
	}
	path, known, imported := r.ResolvePackage(name)
	if !known {
		path = string(name)
	}
	if mod, resolvedPath, ok := r.ModuleByPathOrAlias(path); ok {
		return mod, resolvedPath, true, imported
	}
	if !known {
		return nil, path, false, false
	}
	return nil, path, known, imported
}

func (r *ValidRoot) ModuleByPathOrAlias(prefix string) (*ModuleExports, string, bool) {
	if r == nil || prefix == "" {
		return nil, "", false
	}
	if mod := r.Modules[prefix]; mod != nil {
		return mod, prefix, true
	}
	if path, ok := r.Imports[prefix]; ok {
		if mod := r.Modules[path]; mod != nil {
			return mod, path, true
		}
	}
	if path, ok := r.Discovered[Ident(prefix)]; ok {
		if mod := r.Modules[path]; mod != nil {
			return mod, path, true
		}
	}
	dotted := strings.ReplaceAll(prefix, "/", ".")
	slashed := strings.ReplaceAll(prefix, ".", "/")
	for path, mod := range r.Modules {
		switch {
		case path == dotted || path == slashed:
			return mod, path, true
		case strings.ReplaceAll(path, "/", ".") == prefix:
			return mod, path, true
		case strings.HasSuffix(path, "/"+prefix):
			return mod, path, true
		}
	}
	return nil, "", false
}

func (r *ValidRoot) ResolvePackage(name Ident) (string, bool, bool) {
	if path, ok := r.Imports[string(name)]; ok {
		return path, true, true
	}
	if path, ok := r.Discovered[name]; ok {
		return path, true, false
	}
	return "", false, false
}

func (c *ValidContext) Root() *ValidRoot {
	return c.root
}

func (c *ValidContext) Child(b Node) *ValidContext {
	if b != nil {
		base := b.GetBase()
		base.EnsureID(c)
	}
	if c.current == b {
		return c
	}
	newCtx := &ValidContext{
		root:        c.root,
		parent:      c,
		current:     b,
		vars:        make(map[Ident]GoMiniType),
		closureNode: c.closureNode,
	}
	if b != nil {
		b.GetBase().Scope = newCtx
	}
	return newCtx
}

func (c *ValidContext) WithNode(b Node) *ValidContext {
	if b != nil {
		base := b.GetBase()
		base.EnsureID(c)
	}
	newCtx := &ValidContext{
		root:        c.root,
		parent:      c.parent, // 共享父级
		current:     b,
		vars:        c.vars, // 共享变量映射
		closureNode: c.closureNode,
	}
	if b != nil {
		b.GetBase().Scope = newCtx
	}
	return newCtx
}

type ValidStruct struct {
	Fields    map[Ident]GoMiniType
	Methods   map[Ident]CallFunctionType
	Defined   bool
	Ownership StructOwnership
}

func (s *ValidStruct) IsHostOpaque() bool {
	return s != nil && s.Ownership == StructOwnershipHostOpaque
}

func (c *ValidContext) NextID() uint64 {
	c.root.id++
	return c.root.id
}

func (c *ValidContext) AddErrorf(message string, args ...interface{}) {
	c.addErrorForNode(nil, message, args...)
}

func (c *ValidContext) AddErrorAt(node Node, message string, args ...interface{}) {
	c.addErrorForNode(node, message, args...)
}

func (c *ValidContext) addErrorForNode(node Node, message string, args ...interface{}) {
	path := make([]string, 0)
	msg := fmt.Sprintf(message, args...)
	ctx := c
	var firstNode Node
	for ctx != nil && ctx.current != nil {
		if firstNode == nil && node == nil {
			firstNode = ctx.current
		}
		base := ctx.current.GetBase()
		locStr := ""
		if base.Loc != nil {
			locStr = fmt.Sprintf(":%d:%d", base.Loc.L, base.Loc.C)
		}
		path = append([]string{fmt.Sprintf("%s#%s%s", base.Meta, base.ID, locStr)}, path...)
		ctx = ctx.parent
	}
	if node != nil {
		firstNode = node
	}
	for idx := range c.root.logs {
		log := c.root.logs[idx]
		if log.Message != msg {
			continue
		}
		if sameDiagnosticNode(log.Node, firstNode) {
			return
		}
		if replace, skip := mergeDiagnosticByRange(log.Node, firstNode); skip {
			return
		} else if replace {
			c.root.logs[idx].Node = firstNode
			c.root.logs[idx].Path = path
			return
		}
	}
	c.root.logs = append(c.root.logs, Logs{Node: firstNode, Path: path, Message: msg})
}

func sameDiagnosticNode(a, b Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	aBase := a.GetBase()
	bBase := b.GetBase()
	if aBase == nil || bBase == nil {
		return aBase == bBase
	}
	if aBase.ID != "" && bBase.ID != "" && aBase.ID == bBase.ID {
		return true
	}
	if aBase.Loc == nil || bBase.Loc == nil {
		return false
	}
	return aBase.Loc.F == bBase.Loc.F &&
		aBase.Loc.L == bBase.Loc.L &&
		aBase.Loc.C == bBase.Loc.C &&
		aBase.Loc.EL == bBase.Loc.EL &&
		aBase.Loc.EC == bBase.Loc.EC
}

func mergeDiagnosticByRange(existing, incoming Node) (replace, skip bool) {
	if existing == nil || incoming == nil {
		return false, false
	}
	existingBase := existing.GetBase()
	incomingBase := incoming.GetBase()
	if existingBase == nil || incomingBase == nil || existingBase.Loc == nil || incomingBase.Loc == nil {
		return false, false
	}
	if existingBase.Loc.F != incomingBase.Loc.F {
		return false, false
	}
	if positionContains(existingBase.Loc, incomingBase.Loc) && !positionContains(incomingBase.Loc, existingBase.Loc) {
		return true, false
	}
	if positionContains(incomingBase.Loc, existingBase.Loc) && !positionContains(existingBase.Loc, incomingBase.Loc) {
		return false, true
	}
	return false, false
}

func positionContains(outer, inner *Position) bool {
	if outer == nil || inner == nil {
		return false
	}
	if outer.L > inner.L || outer.EL < inner.EL {
		return false
	}
	if outer.L == inner.L && outer.C > inner.C {
		return false
	}
	if outer.EL == inner.EL && outer.EC < inner.EC {
		return false
	}
	return true
}

func (c *ValidContext) GetType(ident Ident) (GoMiniType, bool) {
	if prefix, member, ok := splitQualifiedMember(string(ident)); ok {
		if mod, _, ok := c.root.ModuleByPathOrAlias(prefix); ok {
			name := Ident(member)
			if t, ok := mod.Types[name]; ok {
				return t, true
			}
			if _, ok := mod.Structs[name]; ok {
				return GoMiniType(mod.Path + "." + member), true
			}
			if _, ok := mod.Interfaces[name]; ok {
				return GoMiniType(mod.Path + "." + member), true
			}
		}
	}
	ctx := c
	for ctx != nil {
		if t, ok := ctx.root.types[ident]; ok {
			return t, true
		}
		ctx = ctx.parent
	}
	var resolved GoMiniType
	for _, mod := range c.root.Modules {
		if t, ok := mod.Types[ident]; ok {
			if resolved != "" {
				return "", false
			}
			resolved = t
		}
	}
	if resolved != "" {
		return resolved, true
	}
	return "", false
}

func (c *ValidContext) GetStruct(ident Ident) (*ValidStruct, bool) {
	if prefix, member, ok := splitQualifiedMember(string(ident)); ok {
		if mod, _, ok := c.root.ModuleByPathOrAlias(prefix); ok {
			if st, ok := mod.StructDefs[Ident(member)]; ok {
				return st, true
			}
		}
	}

	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.root.structs[ident]; ok {
			return miniType, true
		}
		ctx = ctx.parent
	}

	var resolved *ValidStruct
	for _, mod := range c.root.Modules {
		if st, ok := mod.StructDefs[ident]; ok {
			if resolved != nil {
				return nil, false
			}
			resolved = st
		}
	}
	if resolved != nil {
		return resolved, true
	}

	// 在隔离架构下，Array/Map 仅支持基本操作，不再动态生成方法集定义
	// 校验器仅需知道类型存在即可
	if GoMiniType(ident).IsArray() || GoMiniType(ident).IsMap() {
		return &ValidStruct{Fields: make(map[Ident]GoMiniType), Methods: make(map[Ident]CallFunctionType), Ownership: StructOwnershipVMValue}, true
	}

	if GoMiniType(ident).IsPtr() {
		elem, _ := GoMiniType(ident).GetPtrElementType()
		return c.GetStruct(Ident(elem))
	}
	if GoMiniType(ident).IsHostRef() {
		elem, _ := GoMiniType(ident).GetHostRefElementType()
		return c.GetStruct(Ident(elem))
	}
	return nil, false
}

func (c *ValidContext) IsHostOpaqueNamedType(t GoMiniType) bool {
	if c == nil {
		return false
	}
	resolved := t.Resolve(c)
	if resolved.IsPtr() {
		elem, _ := resolved.GetPtrElementType()
		resolved = elem.Resolve(c)
	}
	if resolved.IsHostRef() {
		elem, _ := resolved.GetHostRefElementType()
		resolved = elem
	}
	st, ok := c.GetStruct(Ident(resolved))
	return ok && st.IsHostOpaque()
}

func (c *ValidContext) ContainsHostOpaqueValue(t GoMiniType) bool {
	return c.containsHostOpaqueValue(t.Resolve(c), 0)
}

func (c *ValidContext) containsHostOpaqueValue(t GoMiniType, depth int) bool {
	if c == nil || depth > c.root.MaxTypeDepth {
		return false
	}
	if t.IsHostRef() {
		return false
	}
	if t.IsChan() {
		elem, _ := t.ReadChanElemType()
		return c.containsHostOpaqueValue(elem.Resolve(c), depth+1)
	}
	if t.IsPtr() {
		elem, _ := t.GetPtrElementType()
		return c.IsHostOpaqueNamedType(elem) || c.containsHostOpaqueValue(elem.Resolve(c), depth+1)
	}
	if t.IsArray() {
		elem, _ := t.ReadArrayItemType()
		return c.containsHostOpaqueValue(elem.Resolve(c), depth+1)
	}
	if t.IsMap() {
		k, v, _ := t.GetMapKeyValueTypes()
		return c.containsHostOpaqueValue(k.Resolve(c), depth+1) || c.containsHostOpaqueValue(v.Resolve(c), depth+1)
	}
	if t.IsTuple() {
		items, _ := t.ReadTuple()
		for _, item := range items {
			if c.containsHostOpaqueValue(item.Resolve(c), depth+1) {
				return true
			}
		}
		return false
	}
	if fn, ok := t.ReadFunc(); ok {
		if c.containsHostOpaqueValue(fn.Return.Resolve(c), depth+1) {
			return true
		}
		for _, p := range fn.Params {
			if c.containsHostOpaqueValue(p.Type.Resolve(c), depth+1) {
				return true
			}
		}
		return false
	}
	if t.IsStruct() {
		fields, _ := t.ReadStructFields()
		for _, ft := range fields {
			if c.containsHostOpaqueValue(ft.Resolve(c), depth+1) {
				return true
			}
		}
		return false
	}
	return c.IsHostOpaqueNamedType(t)
}

func (c *ValidContext) GetInterface(ident Ident) (*InterfaceStmt, bool) {
	if prefix, member, ok := splitQualifiedMember(string(ident)); ok {
		if mod, _, ok := c.root.ModuleByPathOrAlias(prefix); ok {
			if it, ok := mod.Interfaces[Ident(member)]; ok {
				return it, true
			}
		}
	}

	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.root.interfaces[ident]; ok {
			return miniType, true
		}
		ctx = ctx.parent
	}

	var resolved *InterfaceStmt
	for _, mod := range c.root.Modules {
		if it, ok := mod.Interfaces[ident]; ok {
			if resolved != nil {
				return nil, false
			}
			resolved = it
		}
	}
	if resolved != nil {
		return resolved, true
	}
	return nil, false
}

func (c *ValidContext) IsAssignableTo(source, target GoMiniType) bool {
	if source.IsAssignableTo(target) {
		return true
	}
	if c == nil {
		return false
	}
	targetInterface, ok := c.resolveInterfaceType(target)
	if !ok {
		return false
	}
	if sourceInterface, ok := c.resolveInterfaceType(source); ok {
		return sourceInterface.IsAssignableTo(targetInterface)
	}
	targetMethods, ok := targetInterface.ReadInterfaceMethods()
	if !ok {
		return false
	}
	receiver := source
	sourceType := source
	if source.IsHostRef() {
		sourceType, _ = source.GetHostRefElementType()
	} else if source.IsPtr() {
		sourceType, _ = source.GetPtrElementType()
	}
	st, ok := c.GetStruct(Ident(sourceType))
	if !ok {
		return false
	}
	for name, expected := range targetMethods {
		actual, ok := st.Methods[Ident(name)]
		if !ok || expected == nil {
			return false
		}
		if !callFunctionTypeAssignable(actual, expected.ToCallFunctionType(), receiver) {
			return false
		}
	}
	return true
}

func (c *ValidContext) resolveInterfaceType(typ GoMiniType) (GoMiniType, bool) {
	if typ.IsInterface() {
		return typ, true
	}
	if stmt, ok := c.GetInterface(Ident(typ)); ok && stmt != nil && stmt.Type.IsInterface() {
		return stmt.Type, true
	}
	if t, ok := c.GetType(Ident(typ)); ok && t.IsInterface() {
		return t, true
	}
	return "", false
}

func callFunctionTypeAssignable(actual, expected CallFunctionType, receiver GoMiniType) bool {
	if callFunctionShapeAssignable(actual, expected) {
		return true
	}
	if len(actual.Params) == 0 || !receiver.IsAssignableTo(actual.Params[0]) {
		return false
	}
	trimmed := actual
	trimmed.Params = append([]GoMiniType(nil), actual.Params[1:]...)
	return callFunctionShapeAssignable(trimmed, expected)
}

func callFunctionShapeAssignable(actual, expected CallFunctionType) bool {
	if !actual.Variadic && expected.Variadic {
		return false
	}
	if !actual.Variadic && len(actual.Params) != len(expected.Params) {
		return false
	}
	for i, expectedParam := range expected.Params {
		actualParam := TypeAny
		if i < len(actual.Params) {
			actualParam = actual.Params[i]
		} else if actual.Variadic && len(actual.Params) > 0 {
			actualParam = actual.Params[len(actual.Params)-1]
		}
		if !expectedParam.IsAssignableTo(actualParam) {
			return false
		}
	}
	if actual.Returns == TypeVoid && expected.Returns == TypeAny {
		return true
	}
	return actual.Returns.IsAssignableTo(expected.Returns)
}

func (c *ValidContext) AddVariable(name Ident, oType GoMiniType) {
	c.vars[name] = oType
	if c.parent == nil {
		c.root.vars[name] = oType
		delete(c.root.readOnlyVars, name)
	}
}

func (c *ValidContext) IsReadOnlyVariable(variable Ident) bool {
	if c == nil || c.root == nil {
		return false
	}
	if c.root.readOnlyVars[variable] {
		return true
	}
	s := string(variable)
	if strings.Contains(s, "/") {
		return c.root.readOnlyVars[Ident(strings.ReplaceAll(s, "/", "."))]
	}
	if strings.Contains(s, ".") {
		return c.root.readOnlyVars[Ident(strings.ReplaceAll(s, ".", "/"))]
	}
	return false
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

func (c *ValidContext) GetConstant(name Ident) (GoMiniType, bool) {
	if c == nil || c.root == nil || c.root.program == nil {
		return "", false
	}
	if _, ok := c.root.program.Constants[string(name)]; !ok {
		return "", false
	}
	if c.root.program.ConstantTypes != nil {
		if typ := c.root.program.ConstantTypes[string(name)]; typ != "" {
			return typ, true
		}
	}
	return "Constant", true
}

func (c *ValidContext) GetVariable(variable Ident) (GoMiniType, bool) {
	if prefix, member, ok := splitQualifiedMember(string(variable)); ok {
		if mod, _, ok := c.root.ModuleByPathOrAlias(prefix); ok {
			if vt, ok := mod.MemberType(Ident(member)); ok {
				return vt, true
			}
		}
	}

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
	if prefix, member, ok := splitQualifiedMember(string(fc)); ok {
		if mod, _, ok := c.root.ModuleByPathOrAlias(prefix); ok {
			if fn := mod.Functions[Ident(member)]; fn != nil {
				sig := fn.FunctionType.ToCallFunctionType()
				return &sig, true
			}
		}
	}

	ctx := c
	for ctx != nil {
		if miniType, ok := ctx.root.Global.Methods[fc]; ok {
			return &miniType, true
		}
		ctx = ctx.parent
	}
	return nil, false
}

func splitQualifiedMember(name string) (prefix, member string, ok bool) {
	idx := strings.LastIndex(name, ".")
	if idx <= 0 || idx+1 >= len(name) {
		return "", "", false
	}
	return name[:idx], name[idx+1:], true
}

func (c *ValidContext) Logs() []Logs { return c.root.logs }

func (c *ValidContext) LogCount() int {
	if c == nil || c.root == nil {
		return 0
	}
	return len(c.root.logs)
}

func ForwardStructuredError(ctx *SemanticContext, node Node, logCount int, err error) bool {
	if err == nil {
		return false
	}
	if ctx == nil {
		return true
	}
	if ctx.LogCount() == logCount {
		if node != nil {
			ctx.AddErrorAt(node, "%s", err.Error())
		} else {
			ctx.AddErrorf("%s", err.Error())
		}
	}
	return true
}

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
		vStru = &ValidStruct{Fields: make(map[Ident]GoMiniType), Methods: make(map[Ident]CallFunctionType), Ownership: StructOwnershipVMValue}
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

func (c *ValidContext) ImportPackage(path string) error {
	// 1. 路径规范化，防止 "../" 注入
	path = strings.Trim(path, " \t\n\r")
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return fmt.Errorf("invalid import path: %s", path)
	}

	if c.root.ModuleLoader == nil {
		if c.root.HasExternalImportPath(path) {
			return nil
		}
		return fmt.Errorf("module not found: %s", path)
	}
	if c.root.Imported[path] {
		return nil
	}

	// 2. 静态验证深度检查，防止栈溢出
	if len(c.root.importStack) >= MaxImportDepth {
		return fmt.Errorf("import depth limit exceeded: %s", path)
	}

	// 3. 循环依赖检查
	for _, p := range c.root.importStack {
		if p == path {
			return fmt.Errorf("circular dependency detected in validation: %s", path)
		}
	}

	c.root.Imported[path] = true

	prog, err := c.root.ModuleLoader(path)
	if err != nil {
		if c.root.HasExternalImportPath(path) {
			return nil
		}
		return err
	}

	// 在隔离的验证上下文中检查导入的程序，不合并符号
	v, _ := NewValidatorWithExternalTypesAndConstTypes(prog, c.root.externalTypes, c.root.externalConsts, c.root.externalConstTypes, c.root.Tolerant)
	v.root.Path = path
	v.SetModuleLoader(c.root.ModuleLoader)
	v.root.importStack = append(append([]string(nil), c.root.importStack...), path) // 传递导入栈
	err = prog.Check(NewSemanticContext(v))

	// 合并子模块验证日志
	if v.root.logs != nil {
		c.root.logs = append(c.root.logs, v.root.logs...)
	}

	if err != nil {
		// 返回简单错误，避免将完整的 MiniAstError 嵌套导致 O(2^N) 的字符串拼接爆炸
		return fmt.Errorf("failed to check package %s", path)
	}

	c.root.RegisterModuleExports(NewModuleExportsFromRoot(path, v.root))
	return nil
}

func (c *ValidContext) SetModuleLoader(loader func(path string) (*ProgramStmt, error)) {
	c.root.ModuleLoader = loader
}

func (c *ValidContext) SetTemplateBuiltins(items map[string]string) {
	if c == nil || c.root == nil {
		return
	}
	c.root.TemplateBuiltins = make(map[string]GoMiniType, len(items))
	for name, spec := range items {
		c.root.TemplateBuiltins[name] = GoMiniType(spec)
	}
}

func checkFuncLit(f *FuncLitExpr, ctx *SemanticContext) error {
	funcCtx := ctx.Child(f)
	funcCtx.closureNode = f

	// 1. 检查参数有效性
	seenParams := make(map[Ident]struct{}, len(f.Params))
	for _, param := range f.Params {
		if param.Name == "" || !param.Name.Valid(funcCtx.ValidContext) {
			return fmt.Errorf("invalid param name: %s", param.Name)
		}
		if param.Name != "_" {
			if _, exists := seenParams[param.Name]; exists {
				return fmt.Errorf("parameter redeclared: %s", param.Name)
			}
			seenParams[param.Name] = struct{}{}
		}
		if param.Type.IsVoid() {
			return fmt.Errorf("%s 不接受 void 类型作为函数参数", param.Name)
		}
	}

	// 2. 创建函数作用域并添加参数
	bodyCtx := funcCtx.Child(f.Body)
	for _, param := range f.Params {
		if param.Name != "" && param.Name != "_" {
			bodyCtx.AddVariable(param.Name, param.Type)
		}
	}

	// 3. 校验函数体
	semBodyCtx := bodyCtx
	logCount := semBodyCtx.LogCount()
	if err := f.Body.Check(semBodyCtx); ForwardStructuredError(semBodyCtx, f.Body, logCount, err) {
		return err
	}

	// 4. 返回路径 analysis
	returnTypes, _ := f.FunctionType.Return.ReadTuple()
	if len(returnTypes) > 0 && !(len(returnTypes) == 1 && returnTypes[0].IsVoid()) {
		analyzer := NewReturnAnalyzer(bodyCtx.ValidContext, f.Return)
		if !analyzer.Analyze(f.Body) {
			analyzer.AddReturnPathErrorsToContext(funcCtx.ValidContext)
			if analyzer.ErrorCount() == 0 {
				funcCtx.AddErrorAt(f.Body, "function literal is missing a return statement")
			}
			return errors.New("function literal return path validation failed")
		}
	}

	return nil
}
