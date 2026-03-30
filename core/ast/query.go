package ast

import (
	"fmt"
	"strings"
)

// Visitor 定义了 AST 遍历者的接口
type Visitor interface {
	Visit(node Node) (w Visitor)
}

// Walk 深度优先遍历 AST 节点
func Walk(v Visitor, node Node) {
	if node == nil || v == nil {
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
		Walk(v, n.Value)
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
	case *StructStmt:
		// 叶子节点
	case *GenDeclStmt, *InterruptStmt, *LiteralExpr, *IdentifierExpr, *ConstRefExpr, *ImportExpr, *BadExpr, *BadStmt:
		// 叶子节点
	}
}

// findNodeVisitor 用于搜索覆盖指定坐标的最小节点
type findNodeVisitor struct {
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
	visitor := &findNodeVisitor{
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
		// 1. 获取左值对象的推导类型
		if t.Object == nil {
			return nil
		}
		objType := t.Object.GetBase().Type
		if objType == "" || objType.IsVoid() || objType.IsAny() {
			// 尝试找定义
			objDef := FindDefinition(root, t.Object, parentMap)
			if objDef != nil {
				objType = objDef.GetBase().Type
			}
		}

		if objType == "" || objType.IsVoid() || objType.IsAny() {
			return nil
		}

		// 2. 找到结构体定义或方法定义
		prog, ok := root.(*ProgramStmt)
		if !ok {
			return nil
		}
		typeName := objType.BaseName()

		// 优先检查是否是方法跳转
		methodName := fmt.Sprintf("__method_%s_%s", typeName, t.Property)
		if fn, ok := prog.Functions[Ident(methodName)]; ok {
			return fn
		}

		// 其次跳转到结构体定义
		if st, ok := prog.Structs[Ident(typeName)]; ok {
			return st
		}
		return nil
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
				// 检查结构体
				if s, ok := prog.Structs[ident.Name]; ok {
					return s
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
		case *ForStmt:
			// 检查 For 循环初始化语句中的变量定义 (如 for i := 0; ...)
			if p.Init != nil {
				if d := findInStmt(p.Init.(Stmt), name); d != nil {
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
		if string(st.Name) == name {
			return st
		}
	case *MultiAssignmentStmt:
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
func FindAllReferences(root, def Node, parentMap map[Node]Node) []Node {
	var refs []Node
	// 确保我们拿到的是真正的定义节点
	if ident, ok := def.(*IdentifierExpr); ok {
		d := FindDefinition(root, ident, parentMap)
		if d != nil {
			def = d
		}
	}

	defBase := def.GetBase()
	if defBase == nil || defBase.Loc == nil {
		return nil
	}

	visitedIDs := make(map[string]bool)

	Walk(funcVisitor(func(node Node) bool {
		if node == nil {
			return true
		}

		base := node.GetBase()
		if base != nil && base.ID != "" {
			if visitedIDs[base.ID] {
				return true
			}
			visitedIDs[base.ID] = true
		}

		// 如果节点本身就是定义节点
		if node == def {
			refs = append(refs, node)
			return true
		}

		// 检查标识符是否指向该定义
		if ident, ok := node.(*IdentifierExpr); ok {
			d := FindDefinition(root, ident, parentMap)
			if d != nil {
				dBase := d.GetBase()
				if dBase != nil && dBase.Loc != nil {
					// 通过位置判断是否是同一个定义
					if dBase.Loc.L == defBase.Loc.L && dBase.Loc.C == defBase.Loc.C {
						refs = append(refs, node)
					}
				}
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
}

// FindHoverInfo 获取符号的悬浮提示信息
func FindHoverInfo(root, target Node, parentMap map[Node]Node) *HoverInfo {
	if target == nil {
		return nil
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
		case *AssignmentStmt:
			if id, ok := d.LHS.(*IdentifierExpr); ok {
				info.Signature = fmt.Sprintf("var %s %s", id.Name, d.GetBase().Type)
			}
		case *GenDeclStmt:
			info.Signature = fmt.Sprintf("var %s %s", d.Name, d.Kind)
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
	return f
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
	"len":     "function(Any) Int64",
	"append":  "function(Array<Any>, Any) Array<Any>",
	"make":    "function(Type, ...Int64) Any",
	"new":     "function(Type) Ptr<Any>",
	"panic":   "function(Any) Void",
	"print":   "function(...Any) Void",
	"println": "function(...Any) Void",
}

// FindCompletionsAt 获取指定位置的代码补全建议
func FindCompletionsAt(root Node, line, col int) []CompletionItem {
	node := FindNodeAt(root, line, col)

	pMap := BuildParentMap(root)

	// 特殊处理：如果找到的是容器节点（如 BlockStmt 或 ProgramStmt），
	// 或者根本没找到节点，说明光标可能紧跟在一个标识符或点号后面。
	// 我们尝试向左偏移 1 到 2 个字符来定位前导标识符。
	if node == nil || node == root {
		for offset := 1; offset <= 2; offset++ {
			if col-offset >= 1 {
				prev := FindNodeAt(root, line, col-offset)
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
			prev := FindNodeAt(root, line, col-1)
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
		if t, ok := ctx.GetVariable(id.Name); ok && t == "Package" {
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
	}

	// 2.2 向上爬升作用域收集局部变量、参数和闭包变量
	curr := ctx
	for curr != nil {
		for name, t := range curr.vars {
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

func getMemberCompletions(ctx *ValidContext, obj Expr) []CompletionItem {
	items := make([]CompletionItem, 0)
	objType := obj.GetBase().Type

	if objType == "" || objType == "Any" { // 也尝试推导 Any 类型
		// 尝试推导
		if id, ok := obj.(*IdentifierExpr); ok {
			if t, ok := ctx.GetVariable(id.Name); ok {
				objType = t
			}
		}
	}

	if objType == "" || objType == "Package" || objType == TypeModule {
		// 检查是否是包名
		if id, ok := obj.(*IdentifierExpr); ok {
			pkgName := string(id.Name)
			realPath, isPkg := ctx.root.Imports[pkgName]
			if !isPkg {
				realPath = pkgName
				// 尝试在 ImportedRoots 中查找后缀匹配的真实路径
				for fullPath := range ctx.root.ImportedRoots {
					if fullPath == pkgName || strings.HasSuffix(fullPath, "/"+pkgName) {
						realPath = fullPath
						break
					}
				}
			}

			// 尝试多种路径格式 (例如 net/http 或 net.http)
			targets := []string{pkgName + ".", realPath + ".", strings.ReplaceAll(realPath, "/", ".") + "."}
			seenSymbols := make(map[string]bool)

			// 1. 查找 Go-source 模块成员 (从导入的子 Root 中提取)
			if srcRoot, ok := ctx.root.ImportedRoots[realPath]; ok {
				for name, t := range srcRoot.vars {
					kind := "var"
					if strings.HasPrefix(string(t), "function") {
						kind = "func"
					}
					items = append(items, CompletionItem{Label: string(name), Kind: kind, Type: t})
					seenSymbols[string(name)] = true
				}
				for name := range srcRoot.structs {
					if !seenSymbols[string(name)] {
						items = append(items, CompletionItem{Label: string(name), Kind: "struct"})
						seenSymbols[string(name)] = true
					}
				}
				for name := range srcRoot.interfaces {
					if !seenSymbols[string(name)] {
						items = append(items, CompletionItem{Label: string(name), Kind: "interface"})
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
		}
		if objType == "Package" || objType == TypeModule {
			return items
		}
	}

	typeName := objType.BaseName()
	// 1. 查找结构体成员
	if st, ok := ctx.root.structs[Ident(typeName)]; ok {
		for f, t := range st.Fields {
			items = append(items, CompletionItem{Label: string(f), Kind: "field", Type: t})
		}
		for m, t := range st.Methods {
			sig := t
			// 剥离接收者以便在补全中显示正确的参数列表
			if objType != "Package" && objType != TypeModule {
				if len(sig.Params) > 0 {
					sig.Params = sig.Params[1:]
				}
			}
			items = append(items, CompletionItem{Label: string(m), Kind: "method", Type: sig.MiniType()})
		}
	}

	// 2. 查找接口成员
	if it, ok := ctx.root.interfaces[Ident(typeName)]; ok {
		if methods, ok := it.Type.ReadInterfaceMethods(); ok {
			for m, t := range methods {
				items = append(items, CompletionItem{Label: m, Kind: "method", Type: t.MiniType()})
			}
		}
	}

	return items
}
