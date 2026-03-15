package ast

import (
	"errors"
	"fmt"
	"reflect"
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
	vars            map[Ident]GoMiniType // 全局变量备份，用于跨作用域查找
	Loader          func(path string) (*ProgramStmt, error)
	Imported        map[string]bool   // 已导入的包路径
	PathToPackage   map[string]string // 路径到包名的映射
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
				// 默认别名是包路径的最后一部分
				// (虽然 ffigo 中已经处理了，但为了健壮性这里也做个检查)
				// 实际上如果在 json 中没有给 alias，可能需要处理。但在 ffigo 中给定了。
				alias = imp.Path // fallback
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
	if err := v.AddNativeStructDefines(StdlibStructs...); err != nil {
		return nil, err
	}
	return v, nil
}

func (c *ValidContext) ImportPackage(path string) error {
	if c.root.Loader == nil {
		return nil
	}
	if c.root.Imported[path] {
		return nil
	}
	c.root.Imported[path] = true

	pkgBlock, err := c.root.Loader(path)
	if err != nil {
		return fmt.Errorf("加载包 %s 失败: %v", path, err)
	}

	c.root.PathToPackage[path] = pkgBlock.Package

	// 记录原始上下文状态
	oldPackage := c.root.Package
	oldImports := c.root.Imports
	oldProgram := c.root.program

	// 准备包的导入映射
	newImports := make(map[string]string)
	for _, imp := range pkgBlock.Imports {
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		newImports[alias] = imp.Path
	}

	// 切换到被导入包的上下文环境进行校验
	c.root.Package = pkgBlock.Package
	c.root.Imports = newImports
	c.root.program = pkgBlock

	// 递归处理该包的导入
	for _, imp := range pkgBlock.Imports {
		if err := c.ImportPackage(imp.Path); err != nil {
			return err
		}
	}

	// 校验被导入包，由于共享同一个 root，其符号会自动注册到 root.structs, root.Methods
	oldParent := c.parent
	c.parent = nil
	semCtx := NewSemanticContext(c)
	err = pkgBlock.Check(semCtx)
	c.parent = oldParent

	if err != nil {
		return fmt.Errorf("校验包 %s 失败: %v", path, err)
	}
	optCtx := NewOptimizeContext(c)
	pkgBlock.Optimize(optCtx)

	// 记录符号以便后续合并
	for k, v := range pkgBlock.Functions {
		c.root.ImportedFuncs[k] = v
	}
	for k, v := range pkgBlock.Structs {
		c.root.ImportedStructs[k] = v
	}
	for k, v := range pkgBlock.Variables {
		c.root.ImportedVars[k] = v
	}
	for k, v := range pkgBlock.Constants {
		c.root.ImportedConsts[k] = v
	}

	// 恢复原始上下文
	c.root.Package = oldPackage
	c.root.Imports = oldImports
	c.root.program = oldProgram

	return nil
}

func (c *ValidContext) SetLoader(loader func(path string) (*ProgramStmt, error)) {
	c.root.Loader = loader
}

func (c *ValidContext) GetImportedFuncs() map[Ident]*FunctionStmt {
	return c.root.ImportedFuncs
}

func (c *ValidContext) GetImportedStructs() map[Ident]*StructStmt {
	return c.root.ImportedStructs
}

func (c *ValidContext) GetImportedVars() map[Ident]Expr {
	return c.root.ImportedVars
}

func (c *ValidContext) GetImportedConsts() map[string]string {
	return c.root.ImportedConsts
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
	Defined bool // 是否已通过 Validate 完整定义
}

func (c *ValidContext) NextID() uint64 {
	c.root.id++
	return c.root.id
}

func (c *ValidContext) AddErrorf(message string, args ...interface{}) {
	path := make([]string, 0)
	msg := fmt.Sprintf(message, args...)

	// 从当前节点开始构建路径
	ctx := c
	for ctx != nil && ctx.current != nil {
		base := ctx.current.GetBase()
		path = append([]string{fmt.Sprintf("%s#%s", reflect.ValueOf(ctx.current).Elem().Type().Name(), base.ID)}, path...)
		ctx = ctx.parent
	}
	c.root.logs = append(c.root.logs, Logs{
		Path:    path,
		Message: msg,
	})
}

func (c *ValidContext) GetStruct(ident Ident) (*ValidStruct, bool) {
	if miniType, ok := c.root.structs[ident]; ok {
		return miniType, true
	}

	// 支持跨包查找：如果名称带点，尝试直接从全局 root 查找
	if strings.Contains(string(ident), ".") {
		if miniType, ok := c.root.structs[ident]; ok {
			return miniType, true
		}
	}

	// todo: 创建单态化类型
	if GoMiniType(ident).IsArray() {
		elemType, ok := GoMiniType(ident).ReadArrayItemType()
		if !ok {
			return nil, false
		}
		arrayStruct := &ValidStruct{
			Fields:  make(map[Ident]GoMiniType),
			Methods: make(map[Ident]CallFunctionType),
		}
		// methods need to include receiver type
		arrayType := GoMiniType(ident)
		callFunc, _ := GoMiniType(fmt.Sprintf("function(%s, Int64) %s", arrayType, elemType)).ReadCallFunc()
		arrayStruct.Methods["get"] = callFunc
		readCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s) Int64", arrayType)).ReadCallFunc()
		arrayStruct.Methods["length"] = readCallFunc
		keysCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s) Array<Int64>", arrayType)).ReadCallFunc()
		arrayStruct.Methods["keys"] = keysCallFunc

		setElemType := elemType
		pushElemType := elemType
		if elemType == "Uint8" {
			setElemType = "Int64"
			pushElemType = "Int64"
		}

		setCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, Int64, %s) Void", arrayType, setElemType)).ReadCallFunc()
		arrayStruct.Methods["set"] = setCallFunc
		pushCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, %s) Void", arrayType, pushElemType)).ReadCallFunc()
		arrayStruct.Methods["push"] = pushCallFunc
		removeCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, Int64) Void", arrayType)).ReadCallFunc()
		arrayStruct.Methods["remove"] = removeCallFunc

		// Register global methods for validation
		for name, method := range arrayStruct.Methods {
			c.root.Methods[Ident(fmt.Sprintf("__obj__%s__%s", arrayType, name))] = method
		}
		c.root.structs[ident] = arrayStruct
		return arrayStruct, true
	}
	if GoMiniType(ident).IsMap() {
		keyType, valueType, ok := GoMiniType(ident).GetMapKeyValueTypes()
		if !ok {
			return nil, false
		}
		mapStruct := &ValidStruct{
			Fields:  make(map[Ident]GoMiniType),
			Methods: make(map[Ident]CallFunctionType),
		}
		mapType := GoMiniType(ident)
		getCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, %s) %s", mapType, keyType, valueType)).ReadCallFunc()
		mapStruct.Methods["get"] = getCallFunc
		putCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, %s, %s) Void", mapType, keyType, valueType)).ReadCallFunc()
		mapStruct.Methods["put"] = putCallFunc
		// Also support 'set' as alias for 'put' for consistency with index assignment conversion
		mapStruct.Methods["set"] = putCallFunc

		removeCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, %s) Void", mapType, keyType)).ReadCallFunc()
		mapStruct.Methods["remove"] = removeCallFunc
		sizeCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s) Int64", mapType)).ReadCallFunc()
		mapStruct.Methods["size"] = sizeCallFunc
		mapStruct.Methods["length"] = sizeCallFunc
		containsCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s, %s) Bool", mapType, keyType)).ReadCallFunc()
		mapStruct.Methods["contains"] = containsCallFunc
		keysCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s) Array<%s>", mapType, keyType)).ReadCallFunc()
		mapStruct.Methods["keys"] = keysCallFunc
		valuesCallFunc, _ := GoMiniType(fmt.Sprintf("function(%s) Array<%s>", mapType, valueType)).ReadCallFunc()
		mapStruct.Methods["values"] = valuesCallFunc

		// Register global methods for validation
		for name, method := range mapStruct.Methods {
			c.root.Methods[Ident(fmt.Sprintf("__obj__%s__%s", mapType, name))] = method
		}
		c.root.structs[ident] = mapStruct
		return mapStruct, true
	}
	if GoMiniType(ident).IsPtr() {
		elementType, _ := GoMiniType(ident).GetPtrElementType()
		return c.GetStruct(Ident(elementType))
	}
	return nil, false
}

func (c *ValidContext) AddVariable(name Ident, oType GoMiniType) {
	c.vars[name] = oType
	// 仅在顶级或混淆后的名称（带点）时同步到全局，防止函数参数污染局部变量生成
	if c.parent == nil || strings.Contains(string(name), ".") {
		c.root.vars[name] = oType
	}
}

func (c *ValidContext) CheckScope(f Node) (Node, bool) {
	valueOf := reflect.ValueOf(f)
	if valueOf.Kind() == reflect.Ptr {
		valueOf = valueOf.Elem()
	}
	of := valueOf.Type()
	item := c
	for {
		if item == nil {
			return nil, false
		}
		ot := reflect.ValueOf(item.current)
		if ot.Kind() == reflect.Ptr {
			ot = ot.Elem()
		}
		t := ot.Type()
		if t.AssignableTo(of) {
			return item.current, true
		}
		item = item.parent
	}
}

func (c *ValidContext) GetVariable(variable Ident) (GoMiniType, bool) {
	ctx := c
	for ctx != nil {
		miniType, ok := ctx.vars[variable]
		if ok {
			return miniType, true
		}
		ctx = ctx.parent
	}

	// 回退 1：检查真正的全局变量 (root.vars)
	if miniType, ok := c.root.vars[variable]; ok {
		return miniType, true
	}

	// 回退 2：如果当前在非 main 包，尝试查找 pkg.variable
	if !strings.Contains(string(variable), ".") && c.root.Package != "" && c.root.Package != "main" {
		mangled := Ident(fmt.Sprintf("%s.%s", c.root.Package, variable))
		if miniType, ok := c.root.vars[mangled]; ok {
			return miniType, true
		}
	}

	return "", false
}

func (c *ValidContext) GetFunction(fc Ident) (*CallFunctionType, bool) {
	miniType, ok := c.root.Methods[fc]
	if ok {
		return &miniType, true
	}
	return nil, false
}

func (c *ValidContext) Logs() []Logs {
	return c.root.logs
}

func (c *ValidContext) AddFuncSpec(name Ident, miniType GoMiniType) error {
	if !miniType.Valid(c) {
		return errors.New(string("invalid operation:" + miniType))
	}
	a, b := miniType.ReadFunc()
	if !b {
		return errors.New("不是合法的函数")
	}

	c.root.Methods[name] = a.ToCallFunctionType()
	return nil
}

func (c *ValidContext) AddStructDefine(name Ident, specs map[Ident]GoMiniType) error {
	vStru, ok := c.root.structs[name]
	if !ok {
		valid := name.Valid(c)
		if !valid {
			return errors.New("invalid identifier")
		}
		vStru = &ValidStruct{
			Fields:  make(map[Ident]GoMiniType),
			Methods: make(map[Ident]CallFunctionType),
		}
		c.root.structs[name] = vStru
	}

	for ident, miniType := range specs {
		if !ident.Valid(c) || !miniType.Valid(c) {
			return fmt.Errorf("invalid member identifier (%s) or type (%s) ", ident, miniType)
		}
		callFunc, b := miniType.ReadCallFunc()
		if b {
			vStru.Methods[ident] = callFunc
			c.root.Methods[Ident(fmt.Sprintf("__obj__%s__%s", name, ident))] = callFunc
		} else {
			vStru.Fields[ident] = miniType
		}
	}
	return nil
}

func (c *ValidContext) AddNativeStructDefines(ts ...any) error {
	types := make([]*NativeStruct, 0, len(ts))

	for _, t := range ts {
		var native *NativeStruct
		if ps, ok := t.(PackageStructWrapper); ok {
			typ := reflect.TypeOf(ps.Stru)
			if typ.Kind() != reflect.Ptr || typ.Elem().Kind() != reflect.Struct {
				return fmt.Errorf("type must be a pointer to struct: %s", typ)
			}
			n, err := ParseNative(typ.Elem())
			if err != nil {
				return err
			}
			// Mangle the name early
			oldNameStr := string(n.StructName)
			mangledName := Ident(fmt.Sprintf("%s.%s", ps.Pkg, ps.Name))
			n.StructName = mangledName
			mangledNameStr := string(mangledName)

			// Update methods signatures to use mangled names for the struct itself
			for i, m := range n.Methods {
				m.Params = mangleCallParams(m.Params, oldNameStr, mangledNameStr)
				m.Returns = GoMiniType(mangleStr(string(m.Returns), oldNameStr, mangledNameStr))
				n.Methods[i] = m
			}
			native = n
		} else {
			typ := reflect.TypeOf(t)
			if typ.Kind() != reflect.Ptr || typ.Elem().Kind() != reflect.Struct {
				return fmt.Errorf("type must be a pointer to struct: %s", typ)
			}
			n, err := ParseNative(typ.Elem())
			if err != nil {
				return err
			}
			native = n
		}
		types = append(types, native)
	}

	// 第一阶段：注册所有定义（占坑）
	for _, native := range types {
		methods := make(map[Ident]CallFunctionType)
		fields := make(map[Ident]GoMiniType)
		for ident, miniType := range native.Fields {
			fields[ident] = miniType
		}
		for ident, miniType := range native.Methods {
			methods[ident] = miniType
		}
		c.root.structs[native.StructName] = &ValidStruct{Fields: fields, Methods: methods}
	}

	// 第二阶段：全量校验
	for _, native := range types {
		vs := c.root.structs[native.StructName]
		for ident, miniType := range vs.Fields {
			if !miniType.Valid(c) {
				return fmt.Errorf("invalid field type: %s.%s <=> %s", native.StructName, ident, miniType)
			}
		}
		for ident, call := range vs.Methods {
			if !call.MiniType().Valid(c) {
				return fmt.Errorf("invalid method signature: %s.%s <=> %s", native.StructName, ident, call.MiniType())
			}
		}
	}

	// 第三阶段：注册全局方法名
	for _, native := range types {
		vs := c.root.structs[native.StructName]
		for ident, call := range vs.Methods {
			c.root.Methods[Ident(fmt.Sprintf("__obj__%s__%s", native.StructName, ident))] = call
		}

		if native.LiteralNew {
			// 修改为接受 Any 以支持变量转换 T(x)
			callFunc, _ := GoMiniType(fmt.Sprintf("function(Any) %s", native.StructName)).ReadCallFunc()
			c.root.Methods[Ident(fmt.Sprintf("__obj__new__%s", native.StructName))] = callFunc
		}
	}
	return nil
}

func mangleCallParams(params []GoMiniType, old, new string) []GoMiniType {
	res := make([]GoMiniType, len(params))
	for i, p := range params {
		res[i] = GoMiniType(mangleStr(string(p), old, new))
	}
	return res
}

func mangleStr(s, old, new string) string {
	// 更加彻底的替换，处理 Ptr<T>, Array<T>, Map<K, V> 等情况
	// 注意：这里假设 old 是一个完整的类型名，不会是其他类型名的子串（如 Int 在 Int64 中）
	// 由于我们的标准类型都是首字母大写且具有一定长度，这个假设基本成立

	res := s
	// 处理泛型包裹
	res = strings.ReplaceAll(res, "<"+old+">", "<"+new+">")
	res = strings.ReplaceAll(res, "<"+old+",", "<"+new+",")
	res = strings.ReplaceAll(res, ", "+old+">", ", "+new+">")
	res = strings.ReplaceAll(res, ","+old+">", ","+new+">")
	res = strings.ReplaceAll(res, ", "+old+",", ", "+new+",")
	res = strings.ReplaceAll(res, ","+old+",", ","+new+",")

	// 处理前缀包裹 (Ptr<old> -> Ptr<new>)
	res = strings.ReplaceAll(res, "Ptr<"+old+">", "Ptr<"+new+">")

	// 处理函数签名中的空格或括号
	res = strings.ReplaceAll(res, " "+old, " "+new)
	res = strings.ReplaceAll(res, "("+old, "("+new)
	res = strings.ReplaceAll(res, ","+old, ","+new)
	res = strings.ReplaceAll(res, ")"+old, ")"+new) // 返回类型

	if res == old {
		return new
	}
	return res
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
