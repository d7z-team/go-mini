package ast

import (
	"fmt"
	"sort"
	"strings"
)

type SignatureHelpInfo struct {
	Signatures      []SignatureInformation
	ActiveSignature int
	ActiveParameter int
}

type SignatureInformation struct {
	Label         string
	Documentation string
	Parameters    []ParameterInformation
}

type ParameterInformation struct {
	Label string
}

type DocumentSymbolInfo struct {
	Name         string
	Detail       string
	Kind         string
	Loc          *Position
	SelectionLoc *Position
	Children     []DocumentSymbolInfo
}

func FindSignatureHelpAtFile(prog *ProgramStmt, file string, line, col int, parentMap map[Node]Node) *SignatureHelpInfo {
	if prog == nil || parentMap == nil {
		return nil
	}
	node := FindNodeAtFile(prog, file, line, col)
	call := enclosingCallExpr(node, parentMap)
	if call == nil {
		return nil
	}
	ctx := findScopeContext(prog, call, parentMap)
	if ctx == nil {
		return nil
	}

	var sig CallFunctionType
	var params []FunctionParam
	var label string
	var doc string
	if def := FindDefinition(prog, call.Func, parentMap); def != nil {
		if fn, ok := def.(*FunctionStmt); ok {
			params = append(params, fn.Params...)
			sig = fn.FunctionType.ToCallFunctionType()
			label = functionSignatureLabel(fn.Name, fn.Params, fn.Return, fn.Variadic)
			doc = fn.Doc
		}
	}
	if label == "" {
		fnType := resolveLSPType(ctx, inferLSPType(ctx, call.Func), 0)
		if parsed, ok := fnType.ReadCallFunc(); ok {
			sig = *parsed
			params = functionParamsFromTypes(parsed.Params)
			label = callSignatureLabel(call.Func, params, parsed.Returns, parsed.Variadic)
		}
	}
	if label == "" {
		return nil
	}

	if member, ok := call.Func.(*MemberExpr); ok {
		if objType := inferLSPObjectType(ctx, inferLSPType(ctx, member.Object), 0); objType != "Package" && objType != TypeModule && len(params) > 0 && len(sig.Params) == len(params) {
			if strings.EqualFold(string(params[0].Name), "self") || params[0].Type.BaseName() == objType.BaseName() {
				params = params[1:]
				sig.Params = sig.Params[1:]
				label = callSignatureLabel(call.Func, params, sig.Returns, sig.Variadic)
			}
		}
	}

	active := activeCallParameter(call, line, col)
	if len(params) == 0 {
		active = 0
	} else if active >= len(params) {
		active = len(params) - 1
	}
	info := SignatureInformation{
		Label:         label,
		Documentation: doc,
		Parameters:    make([]ParameterInformation, 0, len(params)),
	}
	for idx, param := range params {
		name := string(param.Name)
		if name == "" {
			name = fmt.Sprintf("arg%d", idx+1)
		}
		info.Parameters = append(info.Parameters, ParameterInformation{Label: name + " " + string(param.Type)})
	}
	return &SignatureHelpInfo{Signatures: []SignatureInformation{info}, ActiveParameter: active}
}

func FindDocumentSymbolsAtFile(prog *ProgramStmt, file string) []DocumentSymbolInfo {
	if prog == nil {
		return nil
	}
	var symbols []DocumentSymbolInfo
	add := func(symbol DocumentSymbolInfo) {
		if symbol.Loc == nil {
			return
		}
		if file != "" && symbol.Loc.F != "" && symbol.Loc.F != file {
			return
		}
		if symbol.SelectionLoc == nil {
			symbol.SelectionLoc = symbol.Loc
		}
		symbols = append(symbols, symbol)
	}

	for name, loc := range prog.ConstantLocs {
		add(DocumentSymbolInfo{Name: name, Kind: "constant", Loc: loc, SelectionLoc: loc})
	}
	for name, expr := range prog.Variables {
		if _, ok := expr.(*ImportExpr); ok {
			continue
		}
		loc := nodeLoc(expr)
		if loc == nil {
			if decl := findInStmtListForName(prog.Main, string(name)); decl != nil {
				loc = nodeLoc(decl)
			}
		}
		add(DocumentSymbolInfo{Name: string(name), Kind: "var", Detail: string(typeOfNode(expr)), Loc: loc, SelectionLoc: loc})
	}
	for name, loc := range prog.TypeLocs {
		add(DocumentSymbolInfo{Name: string(name), Kind: "type", Detail: string(prog.Types[name]), Loc: loc, SelectionLoc: loc})
	}
	for _, st := range prog.Structs {
		if st == nil {
			continue
		}
		symbol := DocumentSymbolInfo{Name: string(st.Name), Kind: "struct", Detail: string(st.QualifiedName), Loc: nodeLoc(st), SelectionLoc: nodeLoc(st)}
		for _, field := range st.FieldNames {
			fieldLoc := nodeLoc(st)
			if st.FieldLocs != nil && st.FieldLocs[field] != nil {
				fieldLoc = st.FieldLocs[field]
			}
			symbol.Children = append(symbol.Children, DocumentSymbolInfo{Name: string(field), Kind: "field", Detail: string(st.Fields[field]), Loc: fieldLoc, SelectionLoc: fieldLoc})
		}
		add(symbol)
	}
	for _, it := range prog.Interfaces {
		if it == nil {
			continue
		}
		add(DocumentSymbolInfo{Name: string(it.Name), Kind: "interface", Detail: string(it.Type), Loc: nodeLoc(it), SelectionLoc: nodeLoc(it)})
	}
	for _, fn := range prog.Functions {
		if fn == nil {
			continue
		}
		add(DocumentSymbolInfo{Name: string(fn.RegistryName()), Kind: "func", Detail: functionSignatureLabel(fn.Name, fn.Params, fn.Return, fn.Variadic), Loc: nodeLoc(fn), SelectionLoc: nodeLoc(fn)})
	}

	sort.SliceStable(symbols, func(i, j int) bool {
		return comparePositions(symbols[i].Loc, symbols[j].Loc) < 0
	})
	return symbols
}

func enclosingCallExpr(node Node, parentMap map[Node]Node) *CallExprStmt {
	for node != nil {
		if call, ok := node.(*CallExprStmt); ok {
			return call
		}
		node = parentMap[node]
	}
	return nil
}

func activeCallParameter(call *CallExprStmt, line, col int) int {
	active := 0
	for idx, arg := range call.Args {
		loc := nodeLoc(arg)
		if loc == nil {
			continue
		}
		if isInside(line, col, loc) {
			return idx
		}
		if cursorAfter(line, col, loc) {
			active = idx + 1
		}
	}
	return active
}

func cursorAfter(line, col int, loc *Position) bool {
	endLine := loc.EL
	if endLine <= 0 {
		endLine = loc.L
	}
	endCol := loc.EC
	if endCol <= 0 {
		endCol = loc.C
	}
	return line > endLine || line == endLine && col >= endCol
}

func functionParamsFromTypes(types []GoMiniType) []FunctionParam {
	params := make([]FunctionParam, 0, len(types))
	for idx, typ := range types {
		params = append(params, FunctionParam{Name: Ident(fmt.Sprintf("arg%d", idx+1)), Type: typ})
	}
	return params
}

func functionSignatureLabel(name Ident, params []FunctionParam, ret GoMiniType, variadic bool) string {
	if name == "" {
		name = "function"
	}
	return callSignatureLabel(&IdentifierExpr{Name: name}, params, ret, variadic)
}

func callSignatureLabel(fn Node, params []FunctionParam, ret GoMiniType, variadic bool) string {
	name := "function"
	switch n := fn.(type) {
	case *IdentifierExpr:
		if n.Name != "" {
			name = string(n.Name)
		}
	case *ConstRefExpr:
		if n.Name != "" {
			name = string(n.Name)
		}
	case *MemberExpr:
		name = string(n.Property)
	}
	parts := make([]string, 0, len(params))
	for idx, param := range params {
		paramType := param.Type
		prefix := string(param.Name)
		if prefix == "" {
			prefix = fmt.Sprintf("arg%d", idx+1)
		}
		if variadic && idx == len(params)-1 {
			prefix = "..." + prefix
		}
		parts = append(parts, prefix+" "+string(paramType))
	}
	if ret == "" || ret == TypeVoid {
		return fmt.Sprintf("%s(%s)", name, strings.Join(parts, ", "))
	}
	return fmt.Sprintf("%s(%s) %s", name, strings.Join(parts, ", "), ret)
}

func findInStmtListForName(stmts []Stmt, name string) Node {
	for _, stmt := range stmts {
		if found := findInStmt(stmt, name); found != nil {
			return found
		}
	}
	return nil
}

func nodeLoc(node Node) *Position {
	if node == nil || node.GetBase() == nil {
		return nil
	}
	return node.GetBase().Loc
}

func typeOfNode(node Node) GoMiniType {
	if node == nil || node.GetBase() == nil {
		return ""
	}
	return node.GetBase().Type
}

func comparePositions(a, b *Position) int {
	if a == nil || b == nil {
		if a == b {
			return 0
		}
		if a == nil {
			return 1
		}
		return -1
	}
	if a.F != b.F {
		if a.F < b.F {
			return -1
		}
		return 1
	}
	if a.L != b.L {
		return a.L - b.L
	}
	return a.C - b.C
}
