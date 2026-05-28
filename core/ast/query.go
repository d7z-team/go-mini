package ast

import (
	"fmt"
	"strings"
)

// Visitor 定义了 AST 遍历者的接口
type Visitor interface {
	Visit(node Node) (w Visitor)
}

func IsNilNode(node Node) bool {
	if node == nil {
		return true
	}
	switch n := node.(type) {
	case *ProgramStmt:
		return n == nil
	case *InterfaceStmt:
		return n == nil
	case *BlockStmt:
		return n == nil
	case *IfStmt:
		return n == nil
	case *ForStmt:
		return n == nil
	case *ReturnStmt:
		return n == nil
	case *DeferStmt:
		return n == nil
	case *GoStmt:
		return n == nil
	case *SendStmt:
		return n == nil
	case *SelectStmt:
		return n == nil
	case *SelectCase:
		return n == nil
	case *RangeStmt:
		return n == nil
	case *SwitchStmt:
		return n == nil
	case *CaseClause:
		return n == nil
	case *FunctionStmt:
		return n == nil
	case *MultiAssignmentStmt:
		return n == nil
	case *GenDeclStmt:
		return n == nil
	case *AssignmentStmt:
		return n == nil
	case *InterruptStmt:
		return n == nil
	case *TryStmt:
		return n == nil
	case *CatchClause:
		return n == nil
	case *StructStmt:
		return n == nil
	case *IncDecStmt:
		return n == nil
	case *ExpressionStmt:
		return n == nil
	case *IdentifierExpr:
		return n == nil
	case *StarExpr:
		return n == nil
	case *AddressExpr:
		return n == nil
	case *ConstRefExpr:
		return n == nil
	case *TypeAssertExpr:
		return n == nil
	case *ReceiveExpr:
		return n == nil
	case *CallExprStmt:
		return n == nil
	case *MemberExpr:
		return n == nil
	case *CompositeExpr:
		return n == nil
	case *IndexExpr:
		return n == nil
	case *SliceExpr:
		return n == nil
	case *FuncLitExpr:
		return n == nil
	case *BinaryExpr:
		return n == nil
	case *UnaryExpr:
		return n == nil
	case *LiteralExpr:
		return n == nil
	case *ImportExpr:
		return n == nil
	case *BadExpr:
		return n == nil
	case *BadStmt:
		return n == nil
	default:
		return false
	}
}

// Walk 深度优先遍历 AST 节点
func Walk(v Visitor, node Node) {
	if IsNilNode(node) || v == nil {
		return
	}
	if v = v.Visit(node); v == nil {
		return
	}

	switch n := node.(type) {
	case *ProgramStmt:
		for _, stmt := range n.Main {
			Walk(v, stmt)
		}
		for _, stmt := range n.Functions {
			Walk(v, stmt)
		}
		for _, stmt := range n.Structs {
			Walk(v, stmt)
		}
		for _, stmt := range n.Interfaces {
			Walk(v, stmt)
		}
		for _, expr := range n.Variables {
			Walk(v, expr)
		}
	case *BlockStmt:
		for _, stmt := range n.Children {
			Walk(v, stmt)
		}
	case *IfStmt:
		Walk(v, n.Cond)
		Walk(v, n.Body)
		if n.ElseBody != nil {
			Walk(v, n.ElseBody)
		}
	case *ForStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		if n.Cond != nil {
			Walk(v, n.Cond)
		}
		if n.Update != nil {
			Walk(v, n.Update)
		}
		Walk(v, n.Body)
	case *RangeStmt:
		Walk(v, n.X)
		Walk(v, n.Body)
	case *SwitchStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		if n.Tag != nil {
			Walk(v, n.Tag)
		}
		Walk(v, n.Body)
	case *CaseClause:
		for _, expr := range n.List {
			Walk(v, expr)
		}
		for _, stmt := range n.Body {
			Walk(v, stmt)
		}
	case *ReturnStmt:
		for _, expr := range n.Results {
			Walk(v, expr)
		}
	case *FunctionStmt:
		if n.Body != nil {
			Walk(v, n.Body)
		}
	case *CallExprStmt:
		Walk(v, n.Func)
		for _, arg := range n.Args {
			Walk(v, arg)
		}
	case *DeferStmt:
		Walk(v, n.Call)
	case *IncDecStmt:
		Walk(v, n.Operand)
	case *ExpressionStmt:
		Walk(v, n.X)
	case *AssignmentStmt:
		Walk(v, n.LHS)
		Walk(v, n.Value)
	case *MultiAssignmentStmt:
		for _, expr := range n.LHS {
			Walk(v, expr)
		}
		for _, value := range n.Values {
			Walk(v, value)
		}
	case *TryStmt:
		Walk(v, n.Body)
		if n.Catch != nil {
			Walk(v, n.Catch)
		}
		if n.Finally != nil {
			Walk(v, n.Finally)
		}
	case *CatchClause:
		Walk(v, n.Body)
	case *BinaryExpr:
		Walk(v, n.Left)
		Walk(v, n.Right)
	case *UnaryExpr:
		Walk(v, n.Operand)
	case *AddressExpr:
		Walk(v, n.Target)
	case *IndexExpr:
		Walk(v, n.Object)
		Walk(v, n.Index)
	case *MemberExpr:
		Walk(v, n.Object)
	case *SliceExpr:
		Walk(v, n.X)
		if n.Low != nil {
			Walk(v, n.Low)
		}
		if n.High != nil {
			Walk(v, n.High)
		}
	case *CompositeExpr:
		for _, elem := range n.Values {
			if elem.Key != nil {
				Walk(v, elem.Key)
			}
			Walk(v, elem.Value)
		}
	case *FuncLitExpr:
		if n.Body != nil {
			Walk(v, n.Body)
		}
	case *StructStmt, *InterfaceStmt:
		// 叶子节点
	case *GenDeclStmt, *InterruptStmt, *LiteralExpr, *IdentifierExpr, *ConstRefExpr, *ImportExpr, *BadExpr, *BadStmt:
		// 叶子节点
	}
}

// findNodeVisitor 用于搜索覆盖指定坐标的最小节点
type findNodeVisitor struct {
	targetFile string
	targetLine int
	targetCol  int
	bestNode   Node
}

func (v *findNodeVisitor) Visit(node Node) Visitor {
	if node == nil {
		return nil
	}
	base := node.GetBase()
	if base != nil && base.Loc != nil {
		loc := base.Loc

		if v.targetFile != "" && loc.F != "" && loc.F != v.targetFile {
			return v
		}
		if isInside(v.targetLine, v.targetCol, loc) {
			// 如果命中，记录该节点。由于 Walk 是深度优先遍历，
			// 后命中的节点一定是更小的子节点，所以我们直接更新 bestNode。
			v.bestNode = node
			return v
		}
	}
	return v
}

func isInside(line, col int, loc *Position) bool {
	// 起始位置判断
	if line < loc.L || (line == loc.L && col < loc.C) {
		return false
	}
	// 结束位置判断 (如果存在)
	if loc.EL > 0 {
		// End column is exclusive
		if line > loc.EL || (line == loc.EL && col >= loc.EC) {
			return false
		}
	} else {
		// 如果没有结束位置，仅匹配起始位置（fallback）
		if line != loc.L {
			return false
		}
	}
	return true
}

// FindNodeAt 根据行列坐标（基于 1）查找最匹配的 AST 节点
func FindNodeAt(root Node, line, col int) Node {
	return FindNodeAtFile(root, "", line, col)
}

// FindNodeAtFile 根据文件名、行列坐标（基于 1）查找最匹配的 AST 节点
func FindNodeAtFile(root Node, file string, line, col int) Node {
	visitor := &findNodeVisitor{
		targetFile: file,
		targetLine: line,
		targetCol:  col,
	}
	Walk(visitor, root)
	return visitor.bestNode
}

// 父节点索引 (Parent Mapping)

type parentMapVisitor struct {
	parentMap map[Node]Node
	current   Node
}

func (v *parentMapVisitor) Visit(node Node) Visitor {
	if node == nil {
		return nil
	}
	if v.current != nil {
		v.parentMap[node] = v.current
	}
	return &parentMapVisitor{parentMap: v.parentMap, current: node}
}

// BuildParentMap 构建一个 子节点 -> 父节点 的索引
func BuildParentMap(root Node) map[Node]Node {
	parentMap := make(map[Node]Node)
	visitor := &parentMapVisitor{parentMap: parentMap}
	Walk(visitor, root)
	return parentMap
}

func findScopeContext(root, target Node, parentMap map[Node]Node) *ValidContext {
	if target == nil {
		return nil
	}
	curr := target
	for curr != nil {
		if scopeObj := curr.GetBase().Scope; scopeObj != nil {
			if ctx, ok := scopeObj.(*ValidContext); ok {
				return ctx
			}
		}
		if parentMap == nil {
			break
		}
		curr = parentMap[curr]
	}
	if root != nil && root.GetBase() != nil {
		if scopeObj := root.GetBase().Scope; scopeObj != nil {
			if ctx, ok := scopeObj.(*ValidContext); ok {
				return ctx
			}
		}
	}
	return nil
}

func findProgramRoot(ctx *ValidContext) *ProgramStmt {
	if ctx == nil || ctx.root == nil {
		return nil
	}
	return ctx.root.program
}

func virtualFieldDefinition(st *StructStmt, field Ident) Node {
	if st == nil {
		return nil
	}
	fieldType, ok := st.Fields[field]
	if !ok {
		return nil
	}
	loc := st.GetBase().Loc
	if st.FieldLocs != nil && st.FieldLocs[field] != nil {
		loc = st.FieldLocs[field]
	}
	return &IdentifierExpr{
		BaseNode: BaseNode{
			ID:   fmt.Sprintf("field_%s_%s", st.Name, field),
			Meta: "field",
			Type: fieldType,
			Loc:  loc,
		},
		Name: field,
	}
}

func virtualConstantDefinition(prog *ProgramStmt, name string) Node {
	if prog == nil {
		return nil
	}
	val, ok := prog.Constants[name]
	if !ok {
		return nil
	}
	loc := prog.GetBase().Loc
	if prog.ConstantLocs != nil && prog.ConstantLocs[name] != nil {
		loc = prog.ConstantLocs[name]
	}
	return &LiteralExpr{
		BaseNode: BaseNode{
			ID:   "const_" + name,
			Meta: "constant",
			Type: "Constant",
			Loc:  loc,
		},
		Value: val,
	}
}

func virtualTypeDefinition(prog *ProgramStmt, name Ident) Node {
	if prog == nil {
		return nil
	}
	t, ok := prog.Types[name]
	if !ok {
		return nil
	}
	loc := prog.GetBase().Loc
	if prog.TypeLocs != nil && prog.TypeLocs[name] != nil {
		loc = prog.TypeLocs[name]
	}
	return &IdentifierExpr{
		BaseNode: BaseNode{
			ID:   "type_" + string(name),
			Meta: "type",
			Type: t,
			Loc:  loc,
		},
		Name: name,
	}
}

func virtualImportDefinition(prog *ProgramStmt, name Ident, usage Node) Node {
	if prog == nil {
		return nil
	}
	for _, imp := range prog.Imports {
		alias := importSpecAlias(imp)
		if alias != string(name) {
			continue
		}
		loc := prog.GetBase().Loc
		if prog.ImportLocs != nil && prog.ImportLocs[alias] != nil {
			loc = prog.ImportLocs[alias]
		}
		if usage != nil && usage.GetBase() != nil && usage.GetBase().Loc != nil && prog.ImportLocs != nil {
			if usageLoc := prog.ImportLocs[ImportLocationKey(usage.GetBase().Loc.F, alias)]; usageLoc != nil {
				loc = usageLoc
			}
		}
		return &IdentifierExpr{
			BaseNode: BaseNode{
				ID:   "import_" + alias,
				Meta: "import",
				Type: TypeModule,
				Loc:  loc,
			},
			Name: Ident(alias),
		}
	}
	return nil
}

func importSpecAlias(imp ImportSpec) string {
	if imp.Alias != "" {
		return imp.Alias
	}
	parts := strings.Split(imp.Path, "/")
	return parts[len(parts)-1]
}

func definitionKey(node Node) string {
	if node == nil || node.GetBase() == nil {
		return ""
	}
	base := node.GetBase()
	if base.Loc == nil {
		return fmt.Sprintf("%s:%s", base.Meta, base.ID)
	}
	return fmt.Sprintf("%s:%s:%d:%d:%d:%d", base.Meta, base.Loc.F, base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC)
}

func resolveMemberDefinition(ctx *ValidContext, t *MemberExpr) Node {
	if ctx == nil || t == nil || t.Object == nil {
		return nil
	}

	objType := inferLSPObjectType(ctx, inferLSPType(ctx, t.Object), 0)
	if objType == "" || objType.IsVoid() || (objType.IsAny() && objType != "Package" && objType != TypeModule) {
		return nil
	}

	findInProgram := func(prog *ProgramStmt, typeName GoMiniType, property Ident) Node {
		if prog == nil {
			return nil
		}
		baseName := typeName.BaseName()
		methodKey := Ident(baseName + "." + string(property))
		if fn, ok := prog.Functions[methodKey]; ok {
			return fn
		}
		if fn, ok := prog.Functions[property]; ok && (typeName == "Package" || typeName == TypeModule) {
			return fn
		}
		if st, ok := prog.Structs[Ident(baseName)]; ok {
			if fieldDef := virtualFieldDefinition(st, property); fieldDef != nil {
				return fieldDef
			}
			return st
		}
		if it, ok := prog.Interfaces[Ident(baseName)]; ok {
			return it
		}
		if st, ok := prog.Structs[property]; ok && (typeName == "Package" || typeName == TypeModule) {
			return st
		}
		if it, ok := prog.Interfaces[property]; ok && (typeName == "Package" || typeName == TypeModule) {
			return it
		}
		for _, stmt := range prog.Main {
			if d := findInStmt(stmt, string(property)); d != nil && (typeName == "Package" || typeName == TypeModule) {
				return d
			}
		}
		return nil
	}

	if objType == "Package" || objType == TypeModule {
		if id, ok := t.Object.(*IdentifierExpr); ok {
			if module, _, _, _ := ctx.root.ResolveModule(id.Name); module != nil {
				return module.Definition(t.Property)
			}
		}
	}

	candidates := []GoMiniType{objType}
	if resolved := resolveLSPType(ctx, objType, 0); resolved != "" && resolved != objType {
		candidates = append(candidates, resolved)
	}

	if prog := findProgramRoot(ctx); prog != nil {
		for _, candidate := range candidates {
			if def := findInProgram(prog, candidate, t.Property); def != nil {
				return def
			}
		}
	}
	for _, module := range ctx.root.Modules {
		for _, candidate := range candidates {
			if def := module.MethodDefinition(candidate, t.Property); def != nil {
				return def
			}
			if st := module.Structs[Ident(candidate.BaseName())]; st != nil {
				if fieldDef := virtualFieldDefinition(st, t.Property); fieldDef != nil {
					return fieldDef
				}
				return st
			}
			if it := module.Interfaces[Ident(candidate.BaseName())]; it != nil {
				return it
			}
		}
	}
	return nil
}

// 符号定义查找 (Definition Lookup)

// FindDefinition 根据标识符表达式查找其定义的原始位置
func FindDefinition(root, target Node, parentMap map[Node]Node) Node {
	if target == nil || parentMap == nil {
		return nil
	}

	var ident *IdentifierExpr
	switch t := target.(type) {
	case *IdentifierExpr:
		ident = t
	case *ConstRefExpr:
		ident = &IdentifierExpr{Name: t.Name}
	case *MemberExpr:
		return resolveMemberDefinition(findScopeContext(root, target, parentMap), t)
	}

	if ident == nil {
		return nil
	}

	name := string(ident.Name)
	curr := target // 保持原始搜索起点

	for curr != nil {
		parent := parentMap[curr]
		if parent == nil {
			// 已经到达顶级 (ProgramStmt)
			if prog, ok := root.(*ProgramStmt); ok {
				// 优先在 Main 列表中寻找物理声明 (GenDeclStmt)，以保证指针唯一性
				for _, stmt := range prog.Main {
					if d := findInStmt(stmt, name); d != nil {
						return d
					}
				}

				// 检查全局函数
				if f, ok := prog.Functions[ident.Name]; ok {
					return f
				}
				if def := virtualConstantDefinition(prog, string(ident.Name)); def != nil {
					return def
				}
				if def := virtualTypeDefinition(prog, ident.Name); def != nil {
					return def
				}
				if it, ok := prog.Interfaces[ident.Name]; ok {
					return it
				}
				if s, ok := prog.Structs[ident.Name]; ok {
					return s
				}
				if def := virtualImportDefinition(prog, ident.Name, target); def != nil {
					return def
				}
				// 回退：检查 Variables map (可能没有 GenDeclStmt 的老代码)
				if v, ok := prog.Variables[ident.Name]; ok {
					return v
				}
			}
			break
		}

		switch p := parent.(type) {
		case *BlockStmt:
			// 在当前块中寻找之前的声明
			for _, stmt := range p.Children {
				if stmt == curr {
					break // 只寻找当前行之前的定义
				}
				if d := findInStmt(stmt, name); d != nil {
					return d
				}
			}
		case *FunctionStmt:
			// 检查函数参数
			for _, param := range p.Params {
				if string(param.Name) == name {
					return p
				}
			}
		case *RangeStmt:
			// 检查 range 的 key/value
			if string(p.Key) == name || string(p.Value) == name {
				return p
			}
		case *CatchClause:
			if string(p.VarName) == name {
				return p
			}
		case *ForStmt:
			// 检查 For 循环初始化语句中的变量定义 (如 for i := 0; ...)
			if init, ok := p.Init.(Stmt); ok {
				if d := findInStmt(init, name); d != nil {
					return d
				}
			}
		case *SwitchStmt:
			if p.Init != nil {
				if d := findInStmt(p.Init, name); d != nil {
					return d
				}
			}
			if p.Assign != nil {
				if d := findInStmt(p.Assign, name); d != nil {
					return d
				}
			}
		case *FuncLitExpr:
			// 检查匿名函数参数
			for _, param := range p.Params {
				if string(param.Name) == name {
					return p
				}
			}
		}
		curr = parent
	}

	return nil
}

func findInStmt(s Stmt, name string) Node {
	if s == nil {
		return nil
	}
	switch st := s.(type) {
	case *GenDeclStmt:
		for _, binding := range st.Bindings {
			if string(binding.Name) == name {
				return st
			}
		}
	case *AssignmentStmt:
		if st.Kind != AssignDefine {
			return nil
		}
		if ident, ok := st.LHS.(*IdentifierExpr); ok && string(ident.Name) == name {
			return ident
		}
	case *MultiAssignmentStmt:
		if st.Kind != AssignDefine {
			return nil
		}
		for _, lhs := range st.LHS {
			if ident, ok := lhs.(*IdentifierExpr); ok && string(ident.Name) == name {
				return ident
			}
		}
	case *BlockStmt:
		// 如果是 Inner Block (例如 DeclStmt 转换而来)，需要递归查找
		if st.Inner {
			for _, child := range st.Children {
				if d := findInStmt(child, name); d != nil {
					return d
				}
			}
		}
	}
	return nil
}

// FindAllReferences 查找所有引用该定义的地方
func FindAllReferences(root, def Node, parentMap map[Node]Node, includeDeclaration bool) []Node {
	var refs []Node
	if def == nil {
		return nil
	}

	// 确保我们拿到的是真正的定义节点
	switch d := def.(type) {
	case *IdentifierExpr:
		if resolved := FindDefinition(root, d, parentMap); resolved != nil {
			def = resolved
		}
	case *MemberExpr:
		if resolved := FindDefinition(root, d, parentMap); resolved != nil {
			def = resolved
		}
	}

	defBase := def.GetBase()
	if defBase == nil || defBase.Loc == nil {
		return nil
	}

	defKey := definitionKey(def)
	if defKey == "" {
		return nil
	}

	seenRefs := make(map[string]bool)
	appendRef := func(node Node) {
		key := definitionKey(node)
		if key == "" || seenRefs[key] {
			return
		}
		seenRefs[key] = true
		refs = append(refs, node)
	}

	if includeDeclaration {
		appendRef(def)
	}

	Walk(funcVisitor(func(node Node) bool {
		if node == nil {
			return true
		}

		// 检查标识符是否指向该定义
		if ident, ok := node.(*IdentifierExpr); ok {
			d := FindDefinition(root, ident, parentMap)
			if definitionKey(d) == defKey {
				appendRef(node)
			}
		}
		if member, ok := node.(*MemberExpr); ok {
			ctx := findScopeContext(root, member, parentMap)
			d := resolveMemberDefinition(ctx, member)
			if definitionKey(d) == defKey {
				appendRef(node)
			}
		}
		return true
	}), root)
	return refs
}

// HoverInfo 包含悬浮提示所需的信息
type HoverInfo struct {
	Type      GoMiniType `json:"type"`
	Signature string     `json:"signature,omitempty"`
	Doc       string     `json:"doc,omitempty"`
	// Markdown, when set, is returned directly by LSP hover.
	Markdown string `json:"markdown,omitempty"`
}

// FindHoverInfo 获取符号的悬浮提示信息
func FindHoverInfo(root, target Node, parentMap map[Node]Node) *HoverInfo {
	if target == nil {
		return nil
	}

	switch t := target.(type) {
	case *StructStmt:
		return &HoverInfo{
			Type:      t.GetBase().Type,
			Signature: fmt.Sprintf("struct %s", t.Name),
			Doc:       t.Doc,
		}
	case *InterfaceStmt:
		return &HoverInfo{
			Type:      t.Type,
			Signature: fmt.Sprintf("interface %s %s", t.Name, t.Type),
		}
	case *ImportExpr:
		return &HoverInfo{
			Type:      TypeModule,
			Signature: fmt.Sprintf("import %q", t.Path),
		}
	}

	if member, ok := target.(*MemberExpr); ok {
		ctx := findScopeContext(root, target, parentMap)
		if ctx == nil {
			return nil
		}
		memberType := inferLSPType(ctx, member)
		def := resolveMemberDefinition(ctx, member)
		if def == nil {
			return &HoverInfo{Type: memberType}
		}
		switch d := def.(type) {
		case *FunctionStmt:
			return &HoverInfo{
				Type:      memberType,
				Signature: d.FunctionType.ToCallFunctionType().String(),
				Doc:       d.Doc,
			}
		case *IdentifierExpr:
			if d.GetBase().Meta == "field" {
				return &HoverInfo{
					Type:      memberType,
					Signature: fmt.Sprintf("field %s %s", d.Name, d.GetBase().Type),
				}
			}
			return &HoverInfo{Type: memberType}
		case *StructStmt:
			if fieldType, ok := d.Fields[member.Property]; ok {
				return &HoverInfo{
					Type:      fieldType,
					Signature: fmt.Sprintf("field %s %s", member.Property, fieldType),
					Doc:       d.Doc,
				}
			}
			return &HoverInfo{
				Type:      memberType,
				Signature: fmt.Sprintf("struct %s", d.Name),
				Doc:       d.Doc,
			}
		case *InterfaceStmt:
			if methods, ok := d.Type.ReadInterfaceMethods(); ok {
				if sig, ok := methods[string(member.Property)]; ok {
					return &HoverInfo{
						Type:      memberType,
						Signature: string(sig.MiniType()),
					}
				}
			}
			return &HoverInfo{Type: memberType}
		default:
			return &HoverInfo{Type: memberType}
		}
	}

	var ident *IdentifierExpr

	// 针对不同类型的节点，寻找其核心标识符
	switch t := target.(type) {
	case *IdentifierExpr:
		ident = t
	case *ConstRefExpr:
		// 如果是常量引用 (优化后的函数调用等)
		ident = &IdentifierExpr{
			BaseNode: t.BaseNode,
			Name:     t.Name,
		}
	case *CompositeExpr:
		// 结构体实例化 MyStruct{...}，提取类型标识符
		if t.Kind != "" {
			ident = &IdentifierExpr{
				BaseNode: t.BaseNode, // 借用位置信息
				Name:     t.Kind,
			}
		}
	case *MemberExpr:
		// 属性访问 obj.Field，暂不支持，除非我们能推导 obj 类型
	default:
		// 启发式：如果点击的不是标识符，但在其内部能找到标识符，则自动锁定
		Walk(funcVisitor(func(n Node) bool {
			if id, ok := n.(*IdentifierExpr); ok {
				ident = id
				return false // 找到第一个就停止
			}
			return true
		}), target)
	}

	if ident != nil {
		def := FindDefinition(root, ident, parentMap)
		if def == nil {
			// 如果是内置类型或无法找到定义，至少返回类型信息（如果有的话）
			if target.GetBase().Type != "" {
				return &HoverInfo{Type: target.GetBase().Type}
			}
			return nil
		}

		base := def.GetBase()
		if base == nil {
			return nil
		}

		info := &HoverInfo{
			Type: base.Type,
		}

		// 提取文档和签名
		switch d := def.(type) {
		case *FunctionStmt:
			info.Signature = d.FunctionType.ToCallFunctionType().String()
			info.Doc = d.Doc
		case *StructStmt:
			info.Doc = d.Doc
			info.Signature = fmt.Sprintf("struct %s", d.Name)
		case *InterfaceStmt:
			info.Type = d.Type
			info.Signature = fmt.Sprintf("interface %s %s", d.Name, d.Type)
		case *ImportExpr:
			info.Type = TypeModule
			info.Signature = fmt.Sprintf("import %q", d.Path)
		case *IdentifierExpr:
			switch d.GetBase().Meta {
			case "type":
				info.Signature = fmt.Sprintf("type %s %s", d.Name, d.GetBase().Type)
			case "import":
				info.Type = TypeModule
				info.Signature = fmt.Sprintf("import %s", d.Name)
			}
		case *LiteralExpr:
			if d.GetBase().Meta == "constant" {
				info.Signature = "const " + d.Value
			}
		case *AssignmentStmt:
			if id, ok := d.LHS.(*IdentifierExpr); ok {
				info.Signature = fmt.Sprintf("var %s %s", id.Name, d.GetBase().Type)
			}
		case *GenDeclStmt:
			parts := make([]string, 0, len(d.Bindings))
			for _, binding := range d.Bindings {
				if binding.Name == "" {
					continue
				}
				parts = append(parts, fmt.Sprintf("%s %s", binding.Name, binding.Kind))
			}
			info.Signature = "var " + strings.Join(parts, ", ")
		}
		return info
	}
	return nil
}

type funcVisitor func(Node) bool

func (f funcVisitor) Visit(node Node) Visitor {
	if f(node) {
		return f
	}
	return nil
}

// 代码补全 (Code Completion)

// CompletionItem 包含代码补全建议
type CompletionItem struct {
	Label string     `json:"label"`
	Kind  string     `json:"kind"` // var, func, struct, interface, package, keyword, builtin, field, method
	Type  GoMiniType `json:"type,omitempty"`
	Doc   string     `json:"doc,omitempty"`
}

var miniKeywords = []string{
	"package", "import", "func", "var", "type", "struct", "interface",
	"if", "else", "for", "range", "switch", "case", "default",
	"return", "defer", "go", "try", "catch", "finally", "throw",
	"break", "continue", "fallthrough",
}

var miniBuiltins = map[string]string{
	"len":    string(CreateFunctionType([]FunctionParam{{Type: TypeAny}}, TypeInt64, false)),
	"cap":    string(CreateFunctionType([]FunctionParam{{Type: TypeAny}}, TypeInt64, false)),
	"append": string(CreateFunctionType([]FunctionParam{{Type: CreateArrayType(TypeAny)}, {Type: TypeAny}}, CreateArrayType(TypeAny), false)),
	"make":   string(CreateFunctionType([]FunctionParam{{Type: "Type"}, {Type: TypeInt64}}, TypeAny, true)),
	"new":    string(CreateFunctionType([]FunctionParam{{Type: "Type"}}, TypeAny.ToPtr(), false)),
	"panic":  string(CreateFunctionType([]FunctionParam{{Type: TypeAny}}, TypeVoid, false)),
	"close":  string(CreateFunctionType([]FunctionParam{{Type: TypeAny}}, TypeVoid, false)),
}

// FindCompletionsAt 获取指定位置的代码补全建议
func FindCompletionsAt(root Node, line, col int) []CompletionItem {
	return FindCompletionsAtFile(root, "", line, col)
}

// FindCompletionsAtFile 获取指定文件位置的代码补全建议
func FindCompletionsAtFile(root Node, file string, line, col int) []CompletionItem {
	node := FindNodeAtFile(root, file, line, col)

	pMap := BuildParentMap(root)

	// 特殊处理：如果找到的是容器节点（如 BlockStmt 或 ProgramStmt），
	// 或者根本没找到节点，说明光标可能紧跟在一个标识符或点号后面。
	// 我们尝试向左偏移 1 到 2 个字符来定位前导标识符。
	if node == nil || node == root {
		for offset := 1; offset <= 2; offset++ {
			if col-offset >= 1 {
				prev := FindNodeAtFile(root, file, line, col-offset)
				if prev != nil && prev != root {
					node = prev
					break
				}
			}
		}
	} else {
		// 如果找到的是容器节点，也尝试偏移
		switch node.(type) {
		case *BlockStmt, *ProgramStmt:
			prev := FindNodeAtFile(root, file, line, col-1)
			if prev != nil && prev != root {
				node = prev
			}
		}
	}

	if node == nil {
		node = root
	}

	var scopeObj interface{}
	currScopeNode := node
	for currScopeNode != nil {
		scopeObj = currScopeNode.GetBase().Scope
		if scopeObj != nil {
			break
		}
		currScopeNode = pMap[currScopeNode]
	}

	if scopeObj == nil {
		scopeObj = root.GetBase().Scope
	}

	if scopeObj == nil {
		return nil
	}

	ctx, ok := scopeObj.(*ValidContext)
	if !ok {
		return nil
	}

	items := make([]CompletionItem, 0)
	seen := make(map[string]bool)
	visibleLocals := collectVisibleLocalNames(node, parentMapOrEmpty(pMap), line, col)

	// 1. 成员补全 (a.B)
	// 如果当前节点本身就是 MemberExpr
	if sel, ok := node.(*MemberExpr); ok {
		return getMemberCompletions(ctx, sel.Object)
	}

	// 启发式：如果当前节点是 IdentifierExpr，且它的父节点是 MemberExpr 的 Object
	if parent, ok := pMap[node]; ok {
		if sel, ok := parent.(*MemberExpr); ok && sel.Object == node {
			return getMemberCompletions(ctx, sel.Object)
		}
	}

	// 启发式：如果我们正在输入 "fmt."，此时 node 可能是 "fmt" 这个 IdentifierExpr。
	// 虽然我们不知道后面是否有点号，但如果在当前作用域中 "fmt" 是个 Package，
	// 且补全请求就在该标识符紧随其后的位置，通常用户就是想要成员补全。
	if id, ok := node.(*IdentifierExpr); ok {
		if _, known, _ := ctx.root.ResolvePackage(id.Name); known {
			// 只有当光标在标识符之后才触发成员补全
			// 注意：这里是一个近似判断
			return getMemberCompletions(ctx, id)
		}
	}

	// 2. 正常作用域补全

	// 2.1 关键字和内置函数 (仅在非类型上下文中)
	if !node.GetBase().IsType {
		for _, kw := range miniKeywords {
			items = append(items, CompletionItem{Label: kw, Kind: "keyword"})
			seen[kw] = true
		}
		for name, t := range miniBuiltins {
			items = append(items, CompletionItem{Label: name, Kind: "builtin", Type: GoMiniType(t)})
			seen[name] = true
		}
		for name, t := range ctx.root.TemplateBuiltins {
			if !seen[name] {
				items = append(items, CompletionItem{Label: name, Kind: "builtin", Type: t})
				seen[name] = true
			}
		}
	}

	// 2.2 向上爬升作用域收集局部变量、参数和闭包变量
	curr := ctx
	for curr != nil {
		for name, t := range curr.vars {
			if _, ok := visibleLocals[string(name)]; !ok {
				continue
			}
			if !seen[string(name)] {
				kind := "var"
				if strings.HasPrefix(string(t), "function") {
					kind = "func"
				}
				// 如果是类型上下文，只显示类型或模块
				if node.GetBase().IsType && kind != "struct" && kind != "interface" {
					continue
				}
				items = append(items, CompletionItem{Label: string(name), Kind: kind, Type: t})
				seen[string(name)] = true
			}
		}
		curr = curr.parent
	}

	// 2.3 收集全局符号 (FFI, 全局函数, 全局变量)
	for name, t := range ctx.root.vars {
		// 处理包名前缀 (如 os.ReadFile) -> 提取包名作为补全建议
		sName := string(name)
		if idx := strings.Index(sName, "."); idx != -1 {
			pkg := sName[:idx]
			if !seen[pkg] {
				items = append(items, CompletionItem{Label: pkg, Kind: "package"})
				seen[pkg] = true
			}
			continue
		}

		if !seen[sName] {
			kind := "var"
			if string(t) == "Package" {
				kind = "package"
			} else if strings.HasPrefix(string(t), "function") {
				kind = "func"
			}
			if node.GetBase().IsType {
				continue
			}
			items = append(items, CompletionItem{Label: sName, Kind: kind, Type: t})
			seen[sName] = true
		}
	}
	for alias := range ctx.root.Discovered {
		sName := string(alias)
		if seen[sName] {
			continue
		}
		items = append(items, CompletionItem{Label: sName, Kind: "package", Type: "Package"})
		seen[sName] = true
	}

	// 2.3.5 收集常量
	if ctx.root.program != nil {
		for name := range ctx.root.program.Constants {
			if !seen[name] {
				typ := GoMiniType("Constant")
				if ctx.root.program.ConstantTypes != nil && ctx.root.program.ConstantTypes[name] != "" {
					typ = ctx.root.program.ConstantTypes[name]
				}
				items = append(items, CompletionItem{Label: name, Kind: "constant", Type: typ})
				seen[name] = true
			}
		}
	}

	// 2.4 收集结构体和接口 (总是显示)
	for name := range ctx.root.structs {
		if !seen[string(name)] {
			items = append(items, CompletionItem{Label: string(name), Kind: "struct"})
			seen[string(name)] = true
		}
	}
	for name := range ctx.root.interfaces {
		if !seen[string(name)] {
			items = append(items, CompletionItem{Label: string(name), Kind: "interface"})
			seen[string(name)] = true
		}
	}

	return items
}

func parentMapOrEmpty(parentMap map[Node]Node) map[Node]Node {
	if parentMap != nil {
		return parentMap
	}
	return map[Node]Node{}
}

func collectVisibleLocalNames(target Node, parentMap map[Node]Node, line, col int) map[string]struct{} {
	visible := make(map[string]struct{})
	if target == nil {
		return visible
	}

	switch n := target.(type) {
	case *BlockStmt:
		collectDeclaredBeforePositionInBlock(n, line, col, visible)
	case *ProgramStmt:
		collectDeclaredBeforePositionInProgram(n, line, col, visible)
	}

	curr := target
	for curr != nil {
		parent := parentMap[curr]
		if parent == nil {
			break
		}

		switch p := parent.(type) {
		case *BlockStmt:
			collectDeclaredBeforeNodeInBlock(p, curr, visible)
		case *FunctionStmt:
			for _, param := range p.Params {
				if param.Name != "" {
					visible[string(param.Name)] = struct{}{}
				}
			}
		case *FuncLitExpr:
			for _, param := range p.Params {
				if param.Name != "" {
					visible[string(param.Name)] = struct{}{}
				}
			}
		case *RangeStmt:
			if p.Body == curr {
				if p.Key != "" {
					visible[string(p.Key)] = struct{}{}
				}
				if p.Value != "" {
					visible[string(p.Value)] = struct{}{}
				}
			}
		case *ForStmt:
			collectDeclaredNamesInNode(p.Init, visible)
		case *SwitchStmt:
			collectDeclaredNamesInNode(p.Init, visible)
			collectDeclaredNamesInNode(p.Assign, visible)
		case *CatchClause:
			if p.Body == curr && p.VarName != "" {
				visible[string(p.VarName)] = struct{}{}
			}
		}

		curr = parent
	}

	return visible
}

func collectDeclaredBeforePositionInBlock(block *BlockStmt, line, col int, visible map[string]struct{}) {
	for _, stmt := range block.Children {
		if !startsBefore(stmt, line, col) {
			break
		}
		collectDeclaredNamesInStmt(stmt, visible)
	}
}

func collectDeclaredBeforePositionInProgram(prog *ProgramStmt, line, col int, visible map[string]struct{}) {
	for _, stmt := range prog.Main {
		if !startsBefore(stmt, line, col) {
			break
		}
		collectDeclaredNamesInStmt(stmt, visible)
	}
}

func collectDeclaredBeforeNodeInBlock(block *BlockStmt, curr Node, visible map[string]struct{}) {
	for _, stmt := range block.Children {
		if stmt == curr {
			break
		}
		collectDeclaredNamesInStmt(stmt, visible)
	}
}

func startsBefore(node Node, line, col int) bool {
	if node == nil || node.GetBase() == nil || node.GetBase().Loc == nil {
		return false
	}
	loc := node.GetBase().Loc
	if loc.L != line {
		return loc.L < line
	}
	return loc.C < col
}

func collectDeclaredNamesInNode(node Node, visible map[string]struct{}) {
	switch n := node.(type) {
	case *BlockStmt:
		for _, child := range n.Children {
			collectDeclaredNamesInStmt(child, visible)
		}
	case Stmt:
		collectDeclaredNamesInStmt(n, visible)
	}
}

func collectDeclaredNamesInStmt(stmt Stmt, visible map[string]struct{}) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *GenDeclStmt:
		for _, binding := range s.Bindings {
			if binding.Name != "" {
				visible[string(binding.Name)] = struct{}{}
			}
		}
	case *AssignmentStmt:
		if s.Kind == AssignDefine {
			if ident, ok := s.LHS.(*IdentifierExpr); ok && ident.Name != "" {
				visible[string(ident.Name)] = struct{}{}
			}
		}
	case *MultiAssignmentStmt:
		if s.Kind != AssignDefine {
			return
		}
		for _, lhs := range s.LHS {
			if ident, ok := lhs.(*IdentifierExpr); ok && ident.Name != "" {
				visible[string(ident.Name)] = struct{}{}
			}
		}
	case *BlockStmt:
		if s.Inner {
			for _, child := range s.Children {
				collectDeclaredNamesInStmt(child, visible)
			}
		}
	}
}

func getMemberCompletions(ctx *ValidContext, obj Expr) []CompletionItem {
	items := make([]CompletionItem, 0)
	objType := inferLSPObjectType(ctx, inferLSPType(ctx, obj), 0)

	// 如果推导失败或推导出的为 Any，且是 IdentifierExpr，则尝试作为包名处理，下文会进一步检查

	if objType == "" || objType == "Package" || objType == TypeModule {
		// 检查是否是包名
		if id, ok := obj.(*IdentifierExpr); ok {
			pkgName := string(id.Name)
			module, realPath, knownPkg, _ := ctx.root.ResolveModule(id.Name)
			if !knownPkg && module == nil {
				realPath = pkgName
			}

			// 尝试多种路径格式 (例如 net/http 或 net.http)
			targets := []string{pkgName + ".", realPath + ".", strings.ReplaceAll(realPath, "/", ".") + "."}
			seenSymbols := make(map[string]bool)

			// 1. 查找 Go-source 模块显式导出成员
			if module != nil {
				for name, fn := range module.Functions {
					t := TypeAny
					if fn != nil {
						t = fn.FunctionType.MiniType()
					}
					items = append(items, CompletionItem{Label: string(name), Kind: "func", Type: t})
					seenSymbols[string(name)] = true
				}
				for name, t := range module.Vars {
					if seenSymbols[string(name)] {
						continue
					}
					if _, isConst := module.Constants[string(name)]; isConst {
						continue
					}
					items = append(items, CompletionItem{Label: string(name), Kind: "var", Type: t})
					seenSymbols[string(name)] = true
				}
				for name := range module.Constants {
					if !seenSymbols[name] {
						items = append(items, CompletionItem{Label: name, Kind: "constant", Type: "Constant"})
						seenSymbols[name] = true
					}
				}
				for name := range module.Structs {
					if !seenSymbols[string(name)] {
						items = append(items, CompletionItem{Label: string(name), Kind: "struct"})
						seenSymbols[string(name)] = true
					}
				}
				for name := range module.Interfaces {
					if !seenSymbols[string(name)] {
						items = append(items, CompletionItem{Label: string(name), Kind: "interface"})
						seenSymbols[string(name)] = true
					}
				}
				for name, t := range module.Types {
					if !seenSymbols[string(name)] {
						items = append(items, CompletionItem{Label: string(name), Kind: "type", Type: t})
						seenSymbols[string(name)] = true
					}
				}
			}

			// 2. 寻找匹配前缀的全局变量 (FFI)
			for name, t := range ctx.root.vars {
				sName := string(name)
				for _, prefix := range targets {
					if strings.HasPrefix(sName, prefix) {
						label := sName[len(prefix):]
						if label == "" || strings.Contains(label, ".") || seenSymbols[label] {
							continue
						}
						seenSymbols[label] = true
						kind := "var"
						if strings.HasPrefix(string(t), "function") {
							kind = "func"
						}
						items = append(items, CompletionItem{Label: label, Kind: kind, Type: t})
						break
					}
				}
			}
			for name := range ctx.root.externalConsts {
				for _, prefix := range targets {
					if strings.HasPrefix(name, prefix) {
						label := name[len(prefix):]
						if label == "" || strings.Contains(label, ".") || seenSymbols[label] {
							continue
						}
						typ := GoMiniType("Constant")
						if declared := ctx.root.externalConstTypes[name]; declared != "" {
							typ = declared
						}
						seenSymbols[label] = true
						items = append(items, CompletionItem{Label: label, Kind: "constant", Type: typ})
						break
					}
				}
			}
		}
		if objType == "Package" || objType == TypeModule {
			return items
		}
	}

	items = append(items, lspTypeMemberCompletions(ctx, objType)...)

	return items
}

func inferLSPType(ctx *ValidContext, expr Node) GoMiniType {
	return inferLSPTypeRecursive(ctx, expr, 0)
}

func resolveLSPType(ctx *ValidContext, t GoMiniType, depth int) GoMiniType {
	if t.IsEmpty() || depth > 20 {
		return t
	}
	resolved := t.Resolve(ctx)
	if resolved != "" && resolved != t {
		return resolveLSPType(ctx, resolved, depth+1)
	}
	if ctx != nil && ctx.root != nil {
		s := string(t)
		if prefix, member, ok := splitQualifiedMember(s); ok {
			if module, _, ok := ctx.root.ModuleByPathOrAlias(prefix); ok {
				if actual, ok := module.Types[Ident(member)]; ok {
					return resolveLSPType(ctx, actual, depth+1)
				}
			}
		}
		var resolved GoMiniType
		for _, module := range ctx.root.Modules {
			if actual, ok := module.Types[Ident(t)]; ok {
				if resolved != "" {
					return t
				}
				resolved = actual
			}
		}
		if resolved != "" {
			return resolveLSPType(ctx, resolved, depth+1)
		}
	}
	if t.IsTuple() {
		parts, _ := t.ReadTuple()
		out := make([]GoMiniType, len(parts))
		for i, part := range parts {
			out[i] = resolveLSPType(ctx, part, depth+1)
		}
		return CreateTupleType(out...)
	}
	if t.IsArray() {
		elem, _ := t.ReadArrayItemType()
		return CreateArrayType(resolveLSPType(ctx, elem, depth+1))
	}
	if t.IsMap() {
		k, v, _ := t.GetMapKeyValueTypes()
		return CreateMapType(resolveLSPType(ctx, k, depth+1), resolveLSPType(ctx, v, depth+1))
	}
	if t.IsPtr() {
		elem, _ := t.GetPtrElementType()
		return resolveLSPType(ctx, elem, depth+1).ToPtr()
	}
	return t
}

func inferLSPObjectType(ctx *ValidContext, t GoMiniType, depth int) GoMiniType {
	t = resolveLSPType(ctx, t, depth+1)
	if t.IsTuple() {
		if parts, ok := t.ReadTuple(); ok && len(parts) > 0 {
			return inferLSPObjectType(ctx, parts[0], depth+1)
		}
	}
	return t
}

func lspTypeMemberCompletions(ctx *ValidContext, objType GoMiniType) []CompletionItem {
	items := make([]CompletionItem, 0)
	seen := make(map[string]struct{})
	candidates := []GoMiniType{objType}
	if resolved := resolveLSPType(ctx, objType, 0); resolved != "" && resolved != objType {
		candidates = append(candidates, resolved)
	}

	for _, candidate := range candidates {
		typeName := candidate.BaseName()
		if st, ok := ctx.GetStruct(Ident(typeName)); ok {
			for f, t := range st.Fields {
				key := "field:" + string(f)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				items = append(items, CompletionItem{Label: string(f), Kind: "field", Type: t})
			}
			for m, t := range st.Methods {
				key := "method:" + string(m)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				sig := t
				if candidate != "Package" && candidate != TypeModule && len(sig.Params) > 0 {
					sig.Params = sig.Params[1:]
				}
				items = append(items, CompletionItem{Label: string(m), Kind: "method", Type: sig.MiniType()})
			}
		}

		for _, lookup := range []Ident{Ident(typeName), Ident(candidate)} {
			if it, ok := ctx.GetInterface(lookup); ok {
				if methods, ok := it.Type.ReadInterfaceMethods(); ok {
					for m, t := range methods {
						key := "method:" + m
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
						items = append(items, CompletionItem{Label: m, Kind: "method", Type: t.MiniType()})
					}
				}
			}
		}
	}
	return items
}

func inferLSPTypeRecursive(ctx *ValidContext, expr Node, depth int) GoMiniType {
	if expr == nil || depth > 20 {
		return ""
	}
	// 如果已经有确定的类型且不是 Any，则直接使用
	if t := expr.GetBase().Type; t != "" && t != "Any" {
		return resolveLSPType(ctx, t, depth+1)
	}

	switch e := expr.(type) {
	case *IdentifierExpr:
		if t, ok := ctx.GetVariable(e.Name); ok {
			return t
		}
		if t, ok := ctx.GetConstant(e.Name); ok {
			return t
		}
		if _, known, _ := ctx.root.ResolvePackage(e.Name); known {
			return "Package"
		}
	case *MemberExpr:
		objType := inferLSPObjectType(ctx, inferLSPTypeRecursive(ctx, e.Object, depth+1), depth+1)
		if objType == "" {
			return ""
		}
		for _, candidate := range []GoMiniType{objType, resolveLSPType(ctx, objType, depth+1)} {
			typeName := candidate.BaseName()
			if st, ok := ctx.GetStruct(Ident(typeName)); ok {
				if t, ok := st.Fields[e.Property]; ok {
					return resolveLSPType(ctx, t, depth+1)
				}
				if m, ok := st.Methods[e.Property]; ok {
					return resolveLSPType(ctx, m.MiniType(), depth+1)
				}
			}
			for _, lookup := range []Ident{Ident(typeName), Ident(candidate)} {
				if iStmt, ok := ctx.GetInterface(lookup); ok {
					methods, _ := iStmt.Type.ReadInterfaceMethods()
					if sig, ok := methods[string(e.Property)]; ok {
						return resolveLSPType(ctx, sig.MiniType(), depth+1)
					}
				}
			}
		}
		// 3. 处理包成员
		if objType == "Package" || objType == TypeModule {
			if id, ok := e.Object.(*IdentifierExpr); ok {
				if module, path, known, _ := ctx.root.ResolveModule(id.Name); module != nil {
					if t, ok := module.MemberType(e.Property); ok {
						return resolveLSPType(ctx, t, depth+1)
					}
				} else {
					if !known {
						path = string(id.Name)
					}
					// FFI 包成员推导
					fullPath := Ident(strings.ReplaceAll(path, "/", ".") + "." + string(e.Property))
					if t, ok := ctx.GetVariable(fullPath); ok {
						return resolveLSPType(ctx, t, depth+1)
					}
					if t, ok := ctx.getExternalConstant(fullPath); ok {
						return resolveLSPType(ctx, t, depth+1)
					}
				}
			}
		}
	case *CallExprStmt:
		fnType := resolveLSPType(ctx, inferLSPTypeRecursive(ctx, e.Func, depth+1), depth+1)
		if sig, ok := fnType.ReadCallFunc(); ok {
			return resolveLSPType(ctx, sig.Returns, depth+1)
		}
	case *LiteralExpr:
		return resolveLSPType(ctx, e.Type, depth+1)
	}

	return resolveLSPType(ctx, expr.GetBase().Type, depth+1)
}
