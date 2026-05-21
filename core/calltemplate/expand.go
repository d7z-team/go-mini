package calltemplate

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strings"
	"text/template"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/gofrontend"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type ExpandedExpr struct {
	TemplateID string
	Expr       ast.Expr
	SourceSig  *runtime.RuntimeFuncSig
}

const maxTemplateExpansionDepth = 64

type ExpandResult struct {
	Changed                   bool
	Exprs                     []ExpandedExpr
	RemovedCompileOnlyPaths   map[string]struct{}
	RemovedCompileOnlyAliases map[ast.Ident]string
}

func (r *ExpandResult) CheckTypes() error {
	if r == nil {
		return nil
	}
	for _, item := range r.Exprs {
		if item.Expr == nil || item.SourceSig == nil {
			continue
		}
		expected := ast.GoMiniType(item.SourceSig.ReturnType.Raw)
		actual := item.Expr.GetBase().Type
		if expected.IsVoid() {
			if !actual.IsVoid() {
				return fmt.Errorf("call template %s expands to %s, expected Void", item.TemplateID, actual)
			}
			continue
		}
		if !actual.IsAssignableTo(expected) {
			return fmt.Errorf("call template %s expands to %s, expected %s", item.TemplateID, actual, expected)
		}
	}
	return nil
}

func ValidateReservedDeclarations(program *ast.ProgramStmt, registry *Registry) error {
	if program == nil {
		return nil
	}
	var reserved map[string]struct{}
	if registry != nil {
		reserved = registry.ReservedNames()
	}
	check := func(name ast.Ident, what string) error {
		if name == "" || name == "_" {
			return nil
		}
		if strings.HasPrefix(string(name), InternalNamePrefix) {
			return fmt.Errorf("%s %s uses reserved call template prefix %s", what, name, InternalNamePrefix)
		}
		if _, ok := reserved[string(name)]; ok {
			return fmt.Errorf("%s %s conflicts with reserved call template", what, name)
		}
		return nil
	}
	for _, imp := range program.Imports {
		alias := imp.Alias
		if alias == "" {
			alias = path.Base(imp.Path)
		}
		if err := check(ast.Ident(alias), "import alias"); err != nil {
			return err
		}
	}
	for name := range program.Variables {
		if err := check(name, "global variable"); err != nil {
			return err
		}
	}
	for name := range program.Constants {
		if err := check(ast.Ident(name), "constant"); err != nil {
			return err
		}
	}
	for name := range program.Types {
		if err := check(name, "type"); err != nil {
			return err
		}
	}
	for name := range program.Structs {
		if err := check(name, "struct"); err != nil {
			return err
		}
	}
	for name := range program.Interfaces {
		if err := check(name, "interface"); err != nil {
			return err
		}
	}
	for _, fn := range program.Functions {
		if err := validateReservedFunction(fn, check); err != nil {
			return err
		}
	}
	for _, stmt := range program.Main {
		if err := validateReservedStmt(stmt, check); err != nil {
			return err
		}
	}
	return nil
}

func ExpandProgram(program *ast.ProgramStmt, registry *Registry) (*ExpandResult, error) {
	if program == nil || registry == nil {
		return &ExpandResult{}, nil
	}
	exp := &expander{
		program:        program,
		registry:       registry,
		result:         &ExpandResult{},
		syntheticUsed:  make(map[string]string),
		syntheticNames: make(map[string]struct{}),
	}
	for name, expr := range program.Variables {
		next, err := exp.expandExpr(expr)
		if err != nil {
			return nil, err
		}
		program.Variables[name] = next
	}
	for _, fn := range program.Functions {
		if fn != nil && fn.Body != nil {
			body, err := exp.expandStmtList(fn.Body.Children)
			if err != nil {
				return nil, err
			}
			fn.Body.Children = body
		}
	}
	main, err := exp.expandStmtList(program.Main)
	if err != nil {
		return nil, err
	}
	program.Main = main
	changed, err := exp.removeCompileOnlyImports()
	if err != nil {
		return nil, err
	}
	if changed {
		exp.result.Changed = true
	}
	program.SyncTopLevelDeclVariables()
	return exp.result, nil
}

type expander struct {
	program        *ast.ProgramStmt
	registry       *Registry
	result         *ExpandResult
	syntheticUsed  map[string]string
	syntheticNames map[string]struct{}
	stack          []string
}

type renderArg struct {
	Index       int
	Placeholder string
	CallValue   string
	Type        string
}

type renderedCall struct {
	Code string
	Args map[string]ast.Expr
}

func (e *expander) expandStmtList(in []ast.Stmt) ([]ast.Stmt, error) {
	out := make([]ast.Stmt, 0, len(in))
	for _, stmt := range in {
		items, err := e.expandStmt(stmt)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func (e *expander) expandStmt(stmt ast.Stmt) ([]ast.Stmt, error) {
	switch n := stmt.(type) {
	case nil:
		return nil, nil
	case *ast.CallExprStmt:
		return e.expandCallAsStmt(n)
	case *ast.ExpressionStmt:
		if call, ok := n.X.(*ast.CallExprStmt); ok {
			return e.expandCallAsStmt(call)
		}
		expr, err := e.expandExpr(n.X)
		if err != nil {
			return nil, err
		}
		n.X = expr
		return []ast.Stmt{n}, nil
	case *ast.BlockStmt:
		children, err := e.expandStmtList(n.Children)
		if err != nil {
			return nil, err
		}
		n.Children = children
	case *ast.GenDeclStmt:
		for i, expr := range n.Values {
			next, err := e.expandExpr(expr)
			if err != nil {
				return nil, err
			}
			n.Values[i] = next
		}
	case *ast.AssignmentStmt:
		lhs, err := e.expandExpr(n.LHS)
		if err != nil {
			return nil, err
		}
		value, err := e.expandExpr(n.Value)
		if err != nil {
			return nil, err
		}
		n.LHS = lhs
		n.Value = value
	case *ast.MultiAssignmentStmt:
		for i, expr := range n.LHS {
			next, err := e.expandExpr(expr)
			if err != nil {
				return nil, err
			}
			n.LHS[i] = next
		}
		for i, expr := range n.Values {
			next, err := e.expandExpr(expr)
			if err != nil {
				return nil, err
			}
			n.Values[i] = next
		}
	case *ast.ReturnStmt:
		for i, expr := range n.Results {
			next, err := e.expandExpr(expr)
			if err != nil {
				return nil, err
			}
			n.Results[i] = next
		}
	case *ast.IncDecStmt:
		operand, err := e.expandExpr(n.Operand)
		if err != nil {
			return nil, err
		}
		n.Operand = operand
	case *ast.IfStmt:
		cond, err := e.expandExpr(n.Cond)
		if err != nil {
			return nil, err
		}
		n.Cond = cond
		if n.Body != nil {
			body, err := e.expandStmtList(n.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Body.Children = body
		}
		if n.ElseBody != nil {
			other, err := e.expandStmtList(n.ElseBody.Children)
			if err != nil {
				return nil, err
			}
			n.ElseBody.Children = other
		}
	case *ast.ForStmt:
		if n.Init != nil {
			if st, ok := n.Init.(ast.Stmt); ok {
				items, err := e.expandStmt(st)
				if err != nil {
					return nil, err
				}
				n.Init, err = singleStmt("for init", items)
				if err != nil {
					return nil, err
				}
			}
		}
		if n.Cond != nil {
			cond, err := e.expandExpr(n.Cond)
			if err != nil {
				return nil, err
			}
			n.Cond = cond
		}
		if n.Update != nil {
			if st, ok := n.Update.(ast.Stmt); ok {
				items, err := e.expandStmt(st)
				if err != nil {
					return nil, err
				}
				n.Update, err = singleStmt("for update", items)
				if err != nil {
					return nil, err
				}
			}
		}
		if st, ok := n.Body.(ast.Stmt); ok {
			items, err := e.expandStmt(st)
			if err != nil {
				return nil, err
			}
			n.Body, err = singleStmt("for body", items)
			if err != nil {
				return nil, err
			}
		}
	case *ast.RangeStmt:
		x, err := e.expandExpr(n.X)
		if err != nil {
			return nil, err
		}
		n.X = x
		if n.Body != nil {
			body, err := e.expandStmtList(n.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Body.Children = body
		}
	case *ast.SwitchStmt:
		if n.Init != nil {
			items, err := e.expandStmt(n.Init)
			if err != nil {
				return nil, err
			}
			n.Init, err = singleStmt("switch init", items)
			if err != nil {
				return nil, err
			}
		}
		if n.Assign != nil {
			items, err := e.expandStmt(n.Assign)
			if err != nil {
				return nil, err
			}
			n.Assign, err = singleStmt("switch assign", items)
			if err != nil {
				return nil, err
			}
		}
		if n.Tag != nil {
			tag, err := e.expandExpr(n.Tag)
			if err != nil {
				return nil, err
			}
			n.Tag = tag
		}
		if n.Body != nil {
			body, err := e.expandStmtList(n.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Body.Children = body
		}
	case *ast.CaseClause:
		for i, expr := range n.List {
			next, err := e.expandExpr(expr)
			if err != nil {
				return nil, err
			}
			n.List[i] = next
		}
		body, err := e.expandStmtList(n.Body)
		if err != nil {
			return nil, err
		}
		n.Body = body
	case *ast.DeferStmt:
		call, err := e.expandCallForCallOnly(n.Call)
		if err != nil {
			return nil, err
		}
		n.Call = call
	case *ast.GoStmt:
		call, err := e.expandCallForCallOnly(n.Call)
		if err != nil {
			return nil, err
		}
		n.Call = call
	case *ast.TryStmt:
		if n.Body != nil {
			body, err := e.expandStmtList(n.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Body.Children = body
		}
		if n.Catch != nil && n.Catch.Body != nil {
			body, err := e.expandStmtList(n.Catch.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Catch.Body.Children = body
		}
		if n.Finally != nil {
			items, err := e.expandStmtList(n.Finally.Children)
			if err != nil {
				return nil, err
			}
			n.Finally.Children = items
		}
	case *ast.FunctionStmt:
		if n.Body != nil {
			body, err := e.expandStmtList(n.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Body.Children = body
		}
	}
	return []ast.Stmt{stmt}, nil
}

func (e *expander) expandCallAsStmt(call *ast.CallExprStmt) ([]ast.Stmt, error) {
	next, err := e.expandCallChildren(call)
	if err != nil {
		return nil, err
	}
	call = next
	tpl, ok := e.matchCall(call)
	if !ok {
		return []ast.Stmt{call}, nil
	}
	if err := e.enterTemplate(tpl.ID); err != nil {
		return nil, err
	}
	defer e.leaveTemplate()
	rendered, err := e.renderCall(tpl, call)
	if err != nil {
		return nil, err
	}
	if tpl.BodyKind == TemplateStmt {
		e.result.Changed = true
		stmts, err := gofrontend.NewConverter().ConvertStmtsSource(rendered.Code)
		if err != nil {
			return nil, fmt.Errorf("expand call template %s as statements: %w", tpl.ID, err)
		}
		for _, stmt := range stmts {
			if err := replacePlaceholdersStmt(stmt, rendered.Args); err != nil {
				return nil, fmt.Errorf("expand call template %s placeholders: %w", tpl.ID, err)
			}
			copyRootSource(stmt, call)
		}
		return e.expandStmtList(stmts)
	}
	expr, err := gofrontend.NewConverter().ConvertExprSource(rendered.Code)
	if err != nil {
		return nil, fmt.Errorf("expand call template %s as expression: %w", tpl.ID, err)
	}
	e.result.Changed = true
	expr, err = replacePlaceholdersExpr(expr, rendered.Args)
	if err != nil {
		return nil, fmt.Errorf("expand call template %s placeholders: %w", tpl.ID, err)
	}
	copyRootSource(expr, call)
	final, err := e.expandExpr(expr)
	if err != nil {
		return nil, err
	}
	e.result.Exprs = append(e.result.Exprs, ExpandedExpr{TemplateID: tpl.ID, Expr: final, SourceSig: runtime.CloneRuntimeFuncSig(tpl.SourceSig)})
	if st, ok := final.(ast.Stmt); ok {
		return []ast.Stmt{st}, nil
	}
	return []ast.Stmt{&ast.ExpressionStmt{
		BaseNode: ast.BaseNode{ID: call.ID, Meta: "expr_stmt", Loc: call.Loc},
		X:        final,
	}}, nil
}

func (e *expander) expandExpr(expr ast.Expr) (ast.Expr, error) {
	switch n := expr.(type) {
	case nil:
		return nil, nil
	case *ast.CallExprStmt:
		call, err := e.expandCallChildren(n)
		if err != nil {
			return nil, err
		}
		tpl, ok := e.matchCall(call)
		if !ok {
			return call, nil
		}
		if tpl.BodyKind != TemplateExpr {
			return nil, fmt.Errorf("statement call template %s cannot be used as expression", tpl.ID)
		}
		if err := e.enterTemplate(tpl.ID); err != nil {
			return nil, err
		}
		defer e.leaveTemplate()
		rendered, err := e.renderCall(tpl, call)
		if err != nil {
			return nil, err
		}
		next, err := gofrontend.NewConverter().ConvertExprSource(rendered.Code)
		if err != nil {
			return nil, fmt.Errorf("expand call template %s as expression: %w", tpl.ID, err)
		}
		e.result.Changed = true
		next, err = replacePlaceholdersExpr(next, rendered.Args)
		if err != nil {
			return nil, fmt.Errorf("expand call template %s placeholders: %w", tpl.ID, err)
		}
		copyRootSource(next, call)
		final, err := e.expandExpr(next)
		if err != nil {
			return nil, err
		}
		e.result.Exprs = append(e.result.Exprs, ExpandedExpr{TemplateID: tpl.ID, Expr: final, SourceSig: runtime.CloneRuntimeFuncSig(tpl.SourceSig)})
		return final, nil
	case *ast.UnaryExpr:
		x, err := e.expandExpr(n.Operand)
		n.Operand = x
		return n, err
	case *ast.BinaryExpr:
		left, err := e.expandExpr(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := e.expandExpr(n.Right)
		n.Left, n.Right = left, right
		return n, err
	case *ast.MemberExpr:
		obj, err := e.expandExpr(n.Object)
		n.Object = obj
		return n, err
	case *ast.IndexExpr:
		obj, err := e.expandExpr(n.Object)
		if err != nil {
			return nil, err
		}
		idx, err := e.expandExpr(n.Index)
		n.Object, n.Index = obj, idx
		return n, err
	case *ast.SliceExpr:
		x, err := e.expandExpr(n.X)
		if err != nil {
			return nil, err
		}
		low, err := e.expandExpr(n.Low)
		if err != nil {
			return nil, err
		}
		high, err := e.expandExpr(n.High)
		n.X, n.Low, n.High = x, low, high
		return n, err
	case *ast.StarExpr:
		x, err := e.expandExpr(n.X)
		n.X = x
		return n, err
	case *ast.TypeAssertExpr:
		x, err := e.expandExpr(n.X)
		n.X = x
		return n, err
	case *ast.CompositeExpr:
		for i, elem := range n.Values {
			key, err := e.expandExpr(elem.Key)
			if err != nil {
				return nil, err
			}
			val, err := e.expandExpr(elem.Value)
			if err != nil {
				return nil, err
			}
			n.Values[i].Key, n.Values[i].Value = key, val
		}
		return n, nil
	case *ast.FuncLitExpr:
		if n.Body != nil {
			body, err := e.expandStmtList(n.Body.Children)
			if err != nil {
				return nil, err
			}
			n.Body.Children = body
		}
		return n, nil
	default:
		return expr, nil
	}
}

func (e *expander) expandCallChildren(call *ast.CallExprStmt) (*ast.CallExprStmt, error) {
	for i, arg := range call.Args {
		next, err := e.expandExpr(arg)
		if err != nil {
			return nil, err
		}
		call.Args[i] = next
	}
	if member, ok := call.Func.(*ast.MemberExpr); ok {
		obj, err := e.expandExpr(member.Object)
		if err != nil {
			return nil, err
		}
		member.Object = obj
		return call, nil
	}
	fn, err := e.expandExpr(call.Func)
	if err != nil {
		return nil, err
	}
	call.Func = fn
	return call, nil
}

func (e *expander) expandCallForCallOnly(call ast.Expr) (*ast.CallExprStmt, error) {
	c, ok := call.(*ast.CallExprStmt)
	if !ok {
		return nil, errors.New("go/defer only supports call expressions")
	}
	next, err := e.expandExpr(c)
	if err != nil {
		return nil, err
	}
	c, ok = next.(*ast.CallExprStmt)
	if !ok {
		return nil, errors.New("go/defer call template must expand to a call expression")
	}
	return c, nil
}

func (e *expander) matchCall(call *ast.CallExprStmt) (FunctionTemplate, bool) {
	switch fn := call.Func.(type) {
	case *ast.IdentifierExpr:
		return e.registry.Global(string(fn.Name))
	case *ast.ConstRefExpr:
		return e.registry.Global(string(fn.Name))
	case *ast.MemberExpr:
		if fn.ResolvedPackageMember && fn.ResolvedPackagePath != "" {
			return e.registry.PackageMember(fn.ResolvedPackagePath, string(fn.Property))
		}
		if id, ok := fn.Object.(*ast.IdentifierExpr); ok {
			if path, ok := e.syntheticPathForAlias(string(id.Name)); ok {
				return e.registry.PackageMember(path, string(fn.Property))
			}
		}
	}
	return FunctionTemplate{}, false
}

func (e *expander) renderCall(tpl FunctionTemplate, call *ast.CallExprStmt) (renderedCall, error) {
	allowedImports := make(map[string]TemplateImport, len(tpl.Imports))
	for _, imp := range tpl.Imports {
		allowedImports[imp.Path] = imp
	}
	args := make([]renderArg, 0, len(call.Args))
	for i, arg := range call.Args {
		name := e.syntheticIdent(fmt.Sprintf("arg_%d", i))
		callValue := name
		if call.Ellipsis && i == len(call.Args)-1 {
			callValue += "..."
		}
		args = append(args, renderArg{
			Index:       i,
			Placeholder: name,
			CallValue:   callValue,
			Type:        string(arg.GetBase().Type),
		})
	}
	freshNames := make(map[string]string)
	tplParsed, err := template.New(tpl.ID).Funcs(template.FuncMap{
		"pkg": func(importPath string) (string, error) {
			imp, ok := allowedImports[importPath]
			if !ok {
				return "", fmt.Errorf("template %s uses undeclared import %s", tpl.ID, importPath)
			}
			return e.importAlias(importPath, imp.AliasHint)
		},
		"arg": func(index int) (string, error) {
			if index < 0 || index >= len(args) {
				return "", fmt.Errorf("template %s argument index %d out of range", tpl.ID, index)
			}
			return args[index].Placeholder, nil
		},
		"callArg": func(index int) (string, error) {
			if index < 0 || index >= len(args) {
				return "", fmt.Errorf("template %s argument index %d out of range", tpl.ID, index)
			}
			return args[index].CallValue, nil
		},
		"args": func() string {
			parts := make([]string, len(args))
			for i, arg := range args {
				parts[i] = arg.CallValue
			}
			return strings.Join(parts, ", ")
		},
		"argc": func() int {
			return len(args)
		},
		"ellipsis": func() bool {
			return call.Ellipsis
		},
		"fresh": func(name string) string {
			if got, ok := freshNames[name]; ok {
				return got
			}
			got := e.syntheticIdent("tmp_" + name)
			freshNames[name] = got
			return got
		},
	}).Parse(tpl.Body)
	if err != nil {
		return renderedCall{}, fmt.Errorf("parse call template %s: %w", tpl.ID, err)
	}
	if err := rejectTemplateDataAccess(tplParsed.Tree.Root); err != nil {
		return renderedCall{}, fmt.Errorf("parse call template %s: %w", tpl.ID, err)
	}
	var buf bytes.Buffer
	if err := tplParsed.Execute(&buf, nil); err != nil {
		return renderedCall{}, fmt.Errorf("render call template %s: %w", tpl.ID, err)
	}
	argMap := make(map[string]ast.Expr, len(args))
	for _, arg := range args {
		argMap[arg.Placeholder] = call.Args[arg.Index]
	}
	return renderedCall{Code: strings.TrimSpace(buf.String()), Args: argMap}, nil
}

func (e *expander) importAlias(importPath, hint string) (string, error) {
	if alias, ok := e.syntheticUsed[importPath]; ok {
		return alias, nil
	}
	seed := hint
	if seed == "" {
		seed = path.Base(importPath)
	}
	alias := e.syntheticIdent("pkg_" + sanitizeIdentSeed(seed))
	e.syntheticUsed[importPath] = alias
	e.program.Imports = append(e.program.Imports, ast.ImportSpec{
		Alias:       alias,
		Path:        importPath,
		Synthetic:   true,
		CompileOnly: e.registry.CompileOnlyPackage(importPath),
	})
	if e.program.Variables == nil {
		e.program.Variables = make(map[ast.Ident]ast.Expr)
	}
	e.program.Variables[ast.Ident(alias)] = &ast.ImportExpr{
		BaseNode: ast.BaseNode{ID: "tpl_import_" + alias, Meta: "import", Type: ast.TypeModule},
		Path:     importPath,
	}
	return alias, nil
}

func (e *expander) syntheticPathForAlias(alias string) (string, bool) {
	for importPath, got := range e.syntheticUsed {
		if got == alias {
			return importPath, true
		}
	}
	return "", false
}

func (e *expander) aliasTaken(alias, importPath string) bool {
	if alias == "" {
		return true
	}
	if _, ok := e.syntheticNames[alias]; ok {
		return true
	}
	for _, imp := range e.program.Imports {
		got := imp.Alias
		if got == "" {
			got = path.Base(imp.Path)
		}
		if got == alias && imp.Path != importPath {
			return true
		}
	}
	if _, ok := e.program.Variables[ast.Ident(alias)]; ok {
		return true
	}
	return false
}

func (e *expander) syntheticIdent(base string) string {
	base = strings.Trim(base, "_")
	if base == "" {
		base = "value"
	}
	candidate := "__gomini_tpl_" + base
	if !e.aliasTaken(candidate, "") {
		e.syntheticNames[candidate] = struct{}{}
		return candidate
	}
	for i := 1; ; i++ {
		next := fmt.Sprintf("%s_%d", candidate, i)
		if !e.aliasTaken(next, "") {
			e.syntheticNames[next] = struct{}{}
			return next
		}
	}
}

func sanitizeIdentSeed(seed string) string {
	var b strings.Builder
	for i, r := range seed {
		valid := r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9'
		if valid {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "pkg"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "pkg_" + out
	}
	return out
}

func (e *expander) enterTemplate(id string) error {
	for _, active := range e.stack {
		if active == id {
			chain := append(append([]string(nil), e.stack...), id)
			return fmt.Errorf("recursive call template expansion: %s", strings.Join(chain, " -> "))
		}
	}
	if len(e.stack) >= maxTemplateExpansionDepth {
		chain := append(append([]string(nil), e.stack...), id)
		return fmt.Errorf("call template expansion depth exceeds %d: %s", maxTemplateExpansionDepth, strings.Join(chain, " -> "))
	}
	e.stack = append(e.stack, id)
	return nil
}

func (e *expander) leaveTemplate() {
	if len(e.stack) > 0 {
		e.stack = e.stack[:len(e.stack)-1]
	}
}

func (e *expander) removeCompileOnlyImports() (bool, error) {
	if e.program == nil || len(e.program.Imports) == 0 {
		return false, nil
	}
	removeAlias := make(map[ast.Ident]string)
	removePath := make(map[string]struct{})
	imports := e.program.Imports[:0]
	for _, imp := range e.program.Imports {
		if !imp.CompileOnly && !e.registry.CompileOnlyPackage(imp.Path) {
			imports = append(imports, imp)
			continue
		}
		alias := imp.Alias
		if alias == "" {
			alias = path.Base(imp.Path)
		}
		removeAlias[ast.Ident(alias)] = imp.Path
		removePath[imp.Path] = struct{}{}
	}
	if len(removeAlias) == 0 {
		return false, nil
	}
	aliasSet := make(map[ast.Ident]struct{}, len(removeAlias))
	for alias := range removeAlias {
		aliasSet[alias] = struct{}{}
	}
	if err := e.checkCompileOnlyResiduals(aliasSet); err != nil {
		return false, err
	}
	e.program.Imports = imports
	for alias, importPath := range removeAlias {
		if expr, ok := e.program.Variables[alias]; ok {
			if imp, ok := expr.(*ast.ImportExpr); ok && imp.Path == importPath {
				delete(e.program.Variables, alias)
			}
		}
	}
	for alias := range removeAlias {
		delete(e.program.ImportLocs, string(alias))
		for key := range e.program.ImportLocs {
			if strings.HasSuffix(key, "\x1f"+string(alias)) {
				delete(e.program.ImportLocs, key)
			}
		}
	}
	e.result.RemovedCompileOnlyPaths = removePath
	e.result.RemovedCompileOnlyAliases = removeAlias
	return true, nil
}

func (e *expander) checkCompileOnlyResiduals(removeAlias map[ast.Ident]struct{}) error {
	for name, expr := range e.program.Variables {
		if _, removingImport := removeAlias[name]; removingImport {
			if _, ok := expr.(*ast.ImportExpr); ok {
				continue
			}
		}
		if alias := firstResidualAliasExpr(expr, removeAlias); alias != "" {
			return fmt.Errorf("compile-only template package alias %s still has residual expression usage after expansion", alias)
		}
	}
	for _, fn := range e.program.Functions {
		if fn != nil {
			if alias := firstResidualAliasStmt(fn.Body, removeAlias); alias != "" {
				return fmt.Errorf("compile-only template package alias %s still has residual function usage after expansion", alias)
			}
		}
	}
	for _, stmt := range e.program.Main {
		if alias := firstResidualAliasStmt(stmt, removeAlias); alias != "" {
			return fmt.Errorf("compile-only template package alias %s still has residual statement usage after expansion", alias)
		}
	}
	return nil
}

func replacePlaceholdersExpr(expr ast.Expr, args map[string]ast.Expr) (ast.Expr, error) {
	if isNilInterface(expr) || len(args) == 0 {
		return expr, nil
	}
	switch n := expr.(type) {
	case *ast.IdentifierExpr:
		if arg, ok := args[string(n.Name)]; ok {
			return cloneExpr(arg)
		}
	case *ast.ConstRefExpr:
		if arg, ok := args[string(n.Name)]; ok {
			return cloneExpr(arg)
		}
	case *ast.CallExprStmt:
		var err error
		n.Func, err = replacePlaceholdersExpr(n.Func, args)
		if err != nil {
			return nil, err
		}
		n.Args, err = replaceExprSlice(n.Args, args)
		if err != nil {
			return nil, err
		}
	case *ast.UnaryExpr:
		next, err := replacePlaceholdersExpr(n.Operand, args)
		n.Operand = next
		if err != nil {
			return nil, err
		}
	case *ast.BinaryExpr:
		var err error
		n.Left, err = replacePlaceholdersExpr(n.Left, args)
		if err != nil {
			return nil, err
		}
		n.Right, err = replacePlaceholdersExpr(n.Right, args)
		if err != nil {
			return nil, err
		}
	case *ast.MemberExpr:
		next, err := replacePlaceholdersExpr(n.Object, args)
		n.Object = next
		if err != nil {
			return nil, err
		}
	case *ast.IndexExpr:
		var err error
		n.Object, err = replacePlaceholdersExpr(n.Object, args)
		if err != nil {
			return nil, err
		}
		n.Index, err = replacePlaceholdersExpr(n.Index, args)
		if err != nil {
			return nil, err
		}
	case *ast.SliceExpr:
		var err error
		n.X, err = replacePlaceholdersExpr(n.X, args)
		if err != nil {
			return nil, err
		}
		n.Low, err = replacePlaceholdersExpr(n.Low, args)
		if err != nil {
			return nil, err
		}
		n.High, err = replacePlaceholdersExpr(n.High, args)
		if err != nil {
			return nil, err
		}
	case *ast.StarExpr:
		next, err := replacePlaceholdersExpr(n.X, args)
		n.X = next
		if err != nil {
			return nil, err
		}
	case *ast.TypeAssertExpr:
		next, err := replacePlaceholdersExpr(n.X, args)
		n.X = next
		if err != nil {
			return nil, err
		}
	case *ast.CompositeExpr:
		for i, elem := range n.Values {
			key, err := replacePlaceholdersExpr(elem.Key, args)
			if err != nil {
				return nil, err
			}
			value, err := replacePlaceholdersExpr(elem.Value, args)
			if err != nil {
				return nil, err
			}
			n.Values[i].Key, n.Values[i].Value = key, value
		}
	case *ast.FuncLitExpr:
		if err := replacePlaceholdersStmt(n.Body, args); err != nil {
			return nil, err
		}
	}
	return expr, nil
}

func replaceExprSlice(items []ast.Expr, args map[string]ast.Expr) ([]ast.Expr, error) {
	for i, item := range items {
		next, err := replacePlaceholdersExpr(item, args)
		if err != nil {
			return nil, err
		}
		items[i] = next
	}
	return items, nil
}

func replacePlaceholdersStmt(stmt ast.Stmt, args map[string]ast.Expr) error {
	if isNilInterface(stmt) || len(args) == 0 {
		return nil
	}
	switch n := stmt.(type) {
	case *ast.BlockStmt:
		for _, child := range n.Children {
			if err := replacePlaceholdersStmt(child, args); err != nil {
				return err
			}
		}
	case *ast.FunctionStmt:
		return replacePlaceholdersStmt(n.Body, args)
	case *ast.GenDeclStmt:
		var err error
		n.Values, err = replaceExprSlice(n.Values, args)
		if err != nil {
			return err
		}
	case *ast.AssignmentStmt:
		var err error
		n.LHS, err = replacePlaceholdersExpr(n.LHS, args)
		if err != nil {
			return err
		}
		n.Value, err = replacePlaceholdersExpr(n.Value, args)
		if err != nil {
			return err
		}
	case *ast.MultiAssignmentStmt:
		var err error
		n.LHS, err = replaceExprSlice(n.LHS, args)
		if err != nil {
			return err
		}
		n.Values, err = replaceExprSlice(n.Values, args)
		if err != nil {
			return err
		}
	case *ast.ReturnStmt:
		var err error
		n.Results, err = replaceExprSlice(n.Results, args)
		if err != nil {
			return err
		}
	case *ast.CallExprStmt:
		_, err := replacePlaceholdersExpr(n, args)
		return err
	case *ast.ExpressionStmt:
		next, err := replacePlaceholdersExpr(n.X, args)
		n.X = next
		if err != nil {
			return err
		}
	case *ast.IncDecStmt:
		next, err := replacePlaceholdersExpr(n.Operand, args)
		n.Operand = next
		if err != nil {
			return err
		}
	case *ast.IfStmt:
		var err error
		n.Cond, err = replacePlaceholdersExpr(n.Cond, args)
		if err != nil {
			return err
		}
		if err := replacePlaceholdersStmt(asStmt(n.Body), args); err != nil {
			return err
		}
		if err := replacePlaceholdersStmt(asStmt(n.ElseBody), args); err != nil {
			return err
		}
	case *ast.ForStmt:
		if err := replacePlaceholdersStmt(asStmt(n.Init), args); err != nil {
			return err
		}
		var err error
		n.Cond, err = replacePlaceholdersExpr(n.Cond, args)
		if err != nil {
			return err
		}
		if err := replacePlaceholdersStmt(asStmt(n.Update), args); err != nil {
			return err
		}
		if err := replacePlaceholdersStmt(asStmt(n.Body), args); err != nil {
			return err
		}
	case *ast.RangeStmt:
		next, err := replacePlaceholdersExpr(n.X, args)
		n.X = next
		if err != nil {
			return err
		}
		return replacePlaceholdersStmt(n.Body, args)
	case *ast.SwitchStmt:
		if err := replacePlaceholdersStmt(n.Init, args); err != nil {
			return err
		}
		if err := replacePlaceholdersStmt(n.Assign, args); err != nil {
			return err
		}
		var err error
		n.Tag, err = replacePlaceholdersExpr(n.Tag, args)
		if err != nil {
			return err
		}
		return replacePlaceholdersStmt(n.Body, args)
	case *ast.CaseClause:
		var err error
		n.List, err = replaceExprSlice(n.List, args)
		if err != nil {
			return err
		}
		for _, child := range n.Body {
			if err := replacePlaceholdersStmt(child, args); err != nil {
				return err
			}
		}
	case *ast.TryStmt:
		if err := replacePlaceholdersStmt(asStmt(n.Body), args); err != nil {
			return err
		}
		if n.Catch != nil {
			if err := replacePlaceholdersStmt(n.Catch.Body, args); err != nil {
				return err
			}
		}
		return replacePlaceholdersStmt(asStmt(n.Finally), args)
	case *ast.DeferStmt:
		next, err := replacePlaceholdersExpr(n.Call, args)
		n.Call = next
		if err != nil {
			return err
		}
	case *ast.GoStmt:
		next, err := replacePlaceholdersExpr(n.Call, args)
		n.Call = next
		if err != nil {
			return err
		}
	}
	return nil
}

func cloneExpr(expr ast.Expr) (ast.Expr, error) {
	if isNilInterface(expr) {
		return nil, nil
	}
	switch n := expr.(type) {
	case *ast.IdentifierExpr:
		return &ast.IdentifierExpr{BaseNode: cloneBase(n.BaseNode), Name: n.Name}, nil
	case *ast.ConstRefExpr:
		return &ast.ConstRefExpr{BaseNode: cloneBase(n.BaseNode), Name: n.Name}, nil
	case *ast.LiteralExpr:
		return &ast.LiteralExpr{BaseNode: cloneBase(n.BaseNode), Value: n.Value}, nil
	case *ast.ImportExpr:
		return &ast.ImportExpr{BaseNode: cloneBase(n.BaseNode), Path: n.Path}, nil
	case *ast.BadExpr:
		return &ast.BadExpr{BaseNode: cloneBase(n.BaseNode), RawText: n.RawText}, nil
	case *ast.StarExpr:
		x, err := cloneExpr(n.X)
		return &ast.StarExpr{BaseNode: cloneBase(n.BaseNode), X: x}, err
	case *ast.TypeAssertExpr:
		x, err := cloneExpr(n.X)
		return &ast.TypeAssertExpr{BaseNode: cloneBase(n.BaseNode), X: x, Type: n.Type, Multi: n.Multi}, err
	case *ast.UnaryExpr:
		operand, err := cloneExpr(n.Operand)
		return &ast.UnaryExpr{BaseNode: cloneBase(n.BaseNode), Operator: n.Operator, Operand: operand}, err
	case *ast.BinaryExpr:
		left, err := cloneExpr(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := cloneExpr(n.Right)
		return &ast.BinaryExpr{BaseNode: cloneBase(n.BaseNode), Operator: n.Operator, Left: left, Right: right}, err
	case *ast.CallExprStmt:
		fn, err := cloneExpr(n.Func)
		if err != nil {
			return nil, err
		}
		args, err := cloneExprs(n.Args)
		if err != nil {
			return nil, err
		}
		return &ast.CallExprStmt{BaseNode: cloneBase(n.BaseNode), Func: fn, Args: args, Ellipsis: n.Ellipsis}, nil
	case *ast.MemberExpr:
		obj, err := cloneExpr(n.Object)
		return &ast.MemberExpr{BaseNode: cloneBase(n.BaseNode), Object: obj, Property: n.Property, ResolvedPackagePath: n.ResolvedPackagePath, ResolvedPackageMember: n.ResolvedPackageMember}, err
	case *ast.IndexExpr:
		obj, err := cloneExpr(n.Object)
		if err != nil {
			return nil, err
		}
		idx, err := cloneExpr(n.Index)
		return &ast.IndexExpr{BaseNode: cloneBase(n.BaseNode), Object: obj, Index: idx, Multi: n.Multi}, err
	case *ast.SliceExpr:
		x, err := cloneExpr(n.X)
		if err != nil {
			return nil, err
		}
		low, err := cloneExpr(n.Low)
		if err != nil {
			return nil, err
		}
		high, err := cloneExpr(n.High)
		return &ast.SliceExpr{BaseNode: cloneBase(n.BaseNode), X: x, Low: low, High: high}, err
	case *ast.CompositeExpr:
		values := make([]ast.CompositeElement, len(n.Values))
		for i, elem := range n.Values {
			key, err := cloneExpr(elem.Key)
			if err != nil {
				return nil, err
			}
			value, err := cloneExpr(elem.Value)
			if err != nil {
				return nil, err
			}
			values[i] = ast.CompositeElement{Key: key, Value: value}
		}
		return &ast.CompositeExpr{BaseNode: cloneBase(n.BaseNode), Kind: n.Kind, Values: values}, nil
	case *ast.FuncLitExpr:
		body, err := cloneBlock(n.Body)
		if err != nil {
			return nil, err
		}
		return &ast.FuncLitExpr{BaseNode: cloneBase(n.BaseNode), FunctionType: cloneFunctionType(n.FunctionType), Body: body, CaptureNames: append([]string(nil), n.CaptureNames...)}, nil
	default:
		return nil, fmt.Errorf("unsupported template argument expression %T", expr)
	}
}

func cloneExprs(items []ast.Expr) ([]ast.Expr, error) {
	if items == nil {
		return nil, nil
	}
	out := make([]ast.Expr, len(items))
	for i, item := range items {
		cloned, err := cloneExpr(item)
		if err != nil {
			return nil, err
		}
		out[i] = cloned
	}
	return out, nil
}

func cloneStmt(stmt ast.Stmt) (ast.Stmt, error) {
	if isNilInterface(stmt) {
		return nil, nil
	}
	switch n := stmt.(type) {
	case *ast.BlockStmt:
		return cloneBlock(n)
	case *ast.FunctionStmt:
		body, err := cloneBlock(n.Body)
		if err != nil {
			return nil, err
		}
		return &ast.FunctionStmt{
			BaseNode:     cloneBase(n.BaseNode),
			FunctionType: cloneFunctionType(n.FunctionType),
			Scope:        n.Scope,
			Name:         n.Name,
			ReceiverType: n.ReceiverType,
			Body:         body,
			Doc:          n.Doc,
		}, nil
	case *ast.GenDeclStmt:
		values, err := cloneExprs(n.Values)
		if err != nil {
			return nil, err
		}
		return &ast.GenDeclStmt{BaseNode: cloneBase(n.BaseNode), Bindings: append([]ast.VarBinding(nil), n.Bindings...), Values: values}, nil
	case *ast.AssignmentStmt:
		lhs, err := cloneExpr(n.LHS)
		if err != nil {
			return nil, err
		}
		value, err := cloneExpr(n.Value)
		return &ast.AssignmentStmt{BaseNode: cloneBase(n.BaseNode), Kind: n.Kind, LHS: lhs, Value: value}, err
	case *ast.MultiAssignmentStmt:
		lhs, err := cloneExprs(n.LHS)
		if err != nil {
			return nil, err
		}
		values, err := cloneExprs(n.Values)
		return &ast.MultiAssignmentStmt{BaseNode: cloneBase(n.BaseNode), Kind: n.Kind, LHS: lhs, Values: values}, err
	case *ast.ReturnStmt:
		results, err := cloneExprs(n.Results)
		return &ast.ReturnStmt{BaseNode: cloneBase(n.BaseNode), Results: results}, err
	case *ast.CallExprStmt:
		expr, err := cloneExpr(n)
		if err != nil {
			return nil, err
		}
		return expr.(*ast.CallExprStmt), nil
	case *ast.ExpressionStmt:
		x, err := cloneExpr(n.X)
		return &ast.ExpressionStmt{BaseNode: cloneBase(n.BaseNode), X: x}, err
	case *ast.IncDecStmt:
		operand, err := cloneExpr(n.Operand)
		return &ast.IncDecStmt{BaseNode: cloneBase(n.BaseNode), Operand: operand, Operator: n.Operator}, err
	case *ast.IfStmt:
		cond, err := cloneExpr(n.Cond)
		if err != nil {
			return nil, err
		}
		body, err := cloneBlock(n.Body)
		if err != nil {
			return nil, err
		}
		elseBody, err := cloneBlock(n.ElseBody)
		return &ast.IfStmt{BaseNode: cloneBase(n.BaseNode), Cond: cond, Body: body, ElseBody: elseBody}, err
	case *ast.ForStmt:
		init, err := cloneNode(n.Init)
		if err != nil {
			return nil, err
		}
		cond, err := cloneExpr(n.Cond)
		if err != nil {
			return nil, err
		}
		update, err := cloneNode(n.Update)
		if err != nil {
			return nil, err
		}
		body, err := cloneNode(n.Body)
		return &ast.ForStmt{BaseNode: cloneBase(n.BaseNode), Init: init, Cond: cond, Update: update, Body: body}, err
	case *ast.RangeStmt:
		x, err := cloneExpr(n.X)
		if err != nil {
			return nil, err
		}
		body, err := cloneBlock(n.Body)
		return &ast.RangeStmt{BaseNode: cloneBase(n.BaseNode), Key: n.Key, Value: n.Value, X: x, Body: body, Define: n.Define}, err
	case *ast.SwitchStmt:
		init, err := cloneStmt(n.Init)
		if err != nil {
			return nil, err
		}
		assign, err := cloneStmt(n.Assign)
		if err != nil {
			return nil, err
		}
		tag, err := cloneExpr(n.Tag)
		if err != nil {
			return nil, err
		}
		body, err := cloneBlock(n.Body)
		return &ast.SwitchStmt{BaseNode: cloneBase(n.BaseNode), Init: init, Assign: assign, Tag: tag, Body: body, IsType: n.IsType}, err
	case *ast.CaseClause:
		list, err := cloneExprs(n.List)
		if err != nil {
			return nil, err
		}
		body, err := cloneStmts(n.Body)
		return &ast.CaseClause{BaseNode: cloneBase(n.BaseNode), List: list, Body: body}, err
	case *ast.TryStmt:
		body, err := cloneBlock(n.Body)
		if err != nil {
			return nil, err
		}
		catch, err := cloneCatch(n.Catch)
		if err != nil {
			return nil, err
		}
		finally, err := cloneBlock(n.Finally)
		return &ast.TryStmt{BaseNode: cloneBase(n.BaseNode), Body: body, Catch: catch, Finally: finally}, err
	case *ast.DeferStmt:
		call, err := cloneExpr(n.Call)
		return &ast.DeferStmt{BaseNode: cloneBase(n.BaseNode), Call: call}, err
	case *ast.GoStmt:
		call, err := cloneExpr(n.Call)
		return &ast.GoStmt{BaseNode: cloneBase(n.BaseNode), Call: call}, err
	case *ast.InterruptStmt:
		return &ast.InterruptStmt{BaseNode: cloneBase(n.BaseNode), InterruptType: n.InterruptType}, nil
	case *ast.BadStmt:
		return &ast.BadStmt{BaseNode: cloneBase(n.BaseNode), RawText: n.RawText}, nil
	case *ast.InterfaceStmt:
		return &ast.InterfaceStmt{BaseNode: cloneBase(n.BaseNode), Name: n.Name, Type: n.Type}, nil
	case *ast.StructStmt:
		return &ast.StructStmt{BaseNode: cloneBase(n.BaseNode), Name: n.Name, Fields: cloneIdentTypeMap(n.Fields), FieldNames: append([]ast.Ident(nil), n.FieldNames...), FieldLocs: cloneIdentPositionMap(n.FieldLocs), Doc: n.Doc}, nil
	default:
		return nil, fmt.Errorf("unsupported template argument statement %T", stmt)
	}
}

func cloneStmts(items []ast.Stmt) ([]ast.Stmt, error) {
	if items == nil {
		return nil, nil
	}
	out := make([]ast.Stmt, len(items))
	for i, item := range items {
		cloned, err := cloneStmt(item)
		if err != nil {
			return nil, err
		}
		out[i] = cloned
	}
	return out, nil
}

func cloneBlock(block *ast.BlockStmt) (*ast.BlockStmt, error) {
	if block == nil {
		return nil, nil
	}
	children, err := cloneStmts(block.Children)
	if err != nil {
		return nil, err
	}
	return &ast.BlockStmt{BaseNode: cloneBase(block.BaseNode), Children: children, Inner: block.Inner}, nil
}

func cloneCatch(catch *ast.CatchClause) (*ast.CatchClause, error) {
	if catch == nil {
		return nil, nil
	}
	body, err := cloneBlock(catch.Body)
	if err != nil {
		return nil, err
	}
	return &ast.CatchClause{BaseNode: cloneBase(catch.BaseNode), VarName: catch.VarName, Body: body}, nil
}

func cloneNode(node ast.Node) (ast.Node, error) {
	if node == nil || isNilInterface(node) {
		return nil, nil
	}
	if expr, ok := node.(ast.Expr); ok {
		return cloneExpr(expr)
	}
	if stmt, ok := node.(ast.Stmt); ok {
		return cloneStmt(stmt)
	}
	return nil, fmt.Errorf("unsupported template argument node %T", node)
}

func cloneFunctionType(in ast.FunctionType) ast.FunctionType {
	out := ast.FunctionType{
		Params:   append([]ast.FunctionParam(nil), in.Params...),
		Return:   in.Return,
		Variadic: in.Variadic,
	}
	return out
}

func cloneBase(in ast.BaseNode) ast.BaseNode {
	in.Scope = nil
	if in.Loc != nil {
		loc := *in.Loc
		in.Loc = &loc
	}
	return in
}

func cloneIdentTypeMap(in map[ast.Ident]ast.GoMiniType) map[ast.Ident]ast.GoMiniType {
	if in == nil {
		return nil
	}
	out := make(map[ast.Ident]ast.GoMiniType, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIdentPositionMap(in map[ast.Ident]*ast.Position) map[ast.Ident]*ast.Position {
	if in == nil {
		return nil
	}
	out := make(map[ast.Ident]*ast.Position, len(in))
	for k, v := range in {
		if v == nil {
			continue
		}
		pos := *v
		out[k] = &pos
	}
	return out
}

func firstResidualAliasStmt(stmt ast.Stmt, aliases map[ast.Ident]struct{}) string {
	if isNilInterface(stmt) {
		return ""
	}
	switch n := stmt.(type) {
	case *ast.BlockStmt:
		for _, child := range n.Children {
			if alias := firstResidualAliasStmt(child, aliases); alias != "" {
				return alias
			}
		}
	case *ast.FunctionStmt:
		return firstResidualAliasStmt(n.Body, aliases)
	case *ast.GenDeclStmt:
		for _, expr := range n.Values {
			if alias := firstResidualAliasExpr(expr, aliases); alias != "" {
				return alias
			}
		}
	case *ast.AssignmentStmt:
		if alias := firstResidualAliasExpr(n.LHS, aliases); alias != "" {
			return alias
		}
		return firstResidualAliasExpr(n.Value, aliases)
	case *ast.MultiAssignmentStmt:
		for _, expr := range n.LHS {
			if alias := firstResidualAliasExpr(expr, aliases); alias != "" {
				return alias
			}
		}
		for _, expr := range n.Values {
			if alias := firstResidualAliasExpr(expr, aliases); alias != "" {
				return alias
			}
		}
	case *ast.ReturnStmt:
		for _, expr := range n.Results {
			if alias := firstResidualAliasExpr(expr, aliases); alias != "" {
				return alias
			}
		}
	case *ast.CallExprStmt:
		return firstResidualAliasExpr(n, aliases)
	case *ast.ExpressionStmt:
		return firstResidualAliasExpr(n.X, aliases)
	case *ast.IncDecStmt:
		return firstResidualAliasExpr(n.Operand, aliases)
	case *ast.IfStmt:
		if alias := firstResidualAliasExpr(n.Cond, aliases); alias != "" {
			return alias
		}
		if alias := firstResidualAliasStmt(asStmt(n.Body), aliases); alias != "" {
			return alias
		}
		return firstResidualAliasStmt(asStmt(n.ElseBody), aliases)
	case *ast.ForStmt:
		if alias := firstResidualAliasStmt(asStmt(n.Init), aliases); alias != "" {
			return alias
		}
		if alias := firstResidualAliasExpr(n.Cond, aliases); alias != "" {
			return alias
		}
		if alias := firstResidualAliasStmt(asStmt(n.Update), aliases); alias != "" {
			return alias
		}
		return firstResidualAliasStmt(asStmt(n.Body), aliases)
	case *ast.RangeStmt:
		if alias := firstResidualAliasExpr(n.X, aliases); alias != "" {
			return alias
		}
		return firstResidualAliasStmt(n.Body, aliases)
	case *ast.SwitchStmt:
		if alias := firstResidualAliasStmt(n.Init, aliases); alias != "" {
			return alias
		}
		if alias := firstResidualAliasStmt(n.Assign, aliases); alias != "" {
			return alias
		}
		if alias := firstResidualAliasExpr(n.Tag, aliases); alias != "" {
			return alias
		}
		return firstResidualAliasStmt(n.Body, aliases)
	case *ast.CaseClause:
		for _, expr := range n.List {
			if alias := firstResidualAliasExpr(expr, aliases); alias != "" {
				return alias
			}
		}
		for _, child := range n.Body {
			if alias := firstResidualAliasStmt(child, aliases); alias != "" {
				return alias
			}
		}
	case *ast.TryStmt:
		if alias := firstResidualAliasStmt(asStmt(n.Body), aliases); alias != "" {
			return alias
		}
		if n.Catch != nil {
			if alias := firstResidualAliasStmt(n.Catch.Body, aliases); alias != "" {
				return alias
			}
		}
		return firstResidualAliasStmt(asStmt(n.Finally), aliases)
	case *ast.DeferStmt:
		return firstResidualAliasExpr(n.Call, aliases)
	case *ast.GoStmt:
		return firstResidualAliasExpr(n.Call, aliases)
	}
	return ""
}

func firstResidualAliasExpr(expr ast.Expr, aliases map[ast.Ident]struct{}) string {
	if isNilInterface(expr) {
		return ""
	}
	switch n := expr.(type) {
	case *ast.IdentifierExpr:
		if _, ok := aliases[n.Name]; ok {
			return string(n.Name)
		}
	case *ast.ConstRefExpr:
		if _, ok := aliases[n.Name]; ok {
			return string(n.Name)
		}
	case *ast.CallExprStmt:
		if alias := firstResidualAliasExpr(n.Func, aliases); alias != "" {
			return alias
		}
		for _, arg := range n.Args {
			if alias := firstResidualAliasExpr(arg, aliases); alias != "" {
				return alias
			}
		}
	case *ast.UnaryExpr:
		return firstResidualAliasExpr(n.Operand, aliases)
	case *ast.BinaryExpr:
		if alias := firstResidualAliasExpr(n.Left, aliases); alias != "" {
			return alias
		}
		return firstResidualAliasExpr(n.Right, aliases)
	case *ast.MemberExpr:
		return firstResidualAliasExpr(n.Object, aliases)
	case *ast.IndexExpr:
		if alias := firstResidualAliasExpr(n.Object, aliases); alias != "" {
			return alias
		}
		return firstResidualAliasExpr(n.Index, aliases)
	case *ast.SliceExpr:
		if alias := firstResidualAliasExpr(n.X, aliases); alias != "" {
			return alias
		}
		if alias := firstResidualAliasExpr(n.Low, aliases); alias != "" {
			return alias
		}
		return firstResidualAliasExpr(n.High, aliases)
	case *ast.StarExpr:
		return firstResidualAliasExpr(n.X, aliases)
	case *ast.TypeAssertExpr:
		return firstResidualAliasExpr(n.X, aliases)
	case *ast.CompositeExpr:
		for _, elem := range n.Values {
			if alias := firstResidualAliasExpr(elem.Key, aliases); alias != "" {
				return alias
			}
			if alias := firstResidualAliasExpr(elem.Value, aliases); alias != "" {
				return alias
			}
		}
	case *ast.FuncLitExpr:
		return firstResidualAliasStmt(n.Body, aliases)
	}
	return ""
}

func validateReservedFunction(fn *ast.FunctionStmt, check func(ast.Ident, string) error) error {
	if fn == nil {
		return nil
	}
	if err := check(fn.Name, "function"); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if err := check(param.Name, "parameter"); err != nil {
			return err
		}
	}
	return validateReservedStmt(fn.Body, check)
}

func validateReservedStmt(stmt ast.Stmt, check func(ast.Ident, string) error) error {
	if isNilInterface(stmt) {
		return nil
	}
	switch n := stmt.(type) {
	case nil:
		return nil
	case *ast.BlockStmt:
		for _, child := range n.Children {
			if err := validateReservedStmt(child, check); err != nil {
				return err
			}
		}
	case *ast.FunctionStmt:
		return validateReservedFunction(n, check)
	case *ast.StructStmt:
		return check(n.Name, "struct")
	case *ast.InterfaceStmt:
		return check(n.Name, "interface")
	case *ast.GenDeclStmt:
		for _, binding := range n.Bindings {
			if err := check(binding.Name, "variable"); err != nil {
				return err
			}
		}
		for _, expr := range n.Values {
			if err := validateReservedExpr(expr, check); err != nil {
				return err
			}
		}
	case *ast.AssignmentStmt:
		if n.Kind == ast.AssignDefine {
			if id, ok := n.LHS.(*ast.IdentifierExpr); ok {
				if err := check(id.Name, "variable"); err != nil {
					return err
				}
			}
		}
		if err := validateReservedExpr(n.LHS, check); err != nil {
			return err
		}
		if err := validateReservedExpr(n.Value, check); err != nil {
			return err
		}
	case *ast.MultiAssignmentStmt:
		if n.Kind == ast.AssignDefine {
			for _, lhs := range n.LHS {
				if id, ok := lhs.(*ast.IdentifierExpr); ok {
					if err := check(id.Name, "variable"); err != nil {
						return err
					}
				}
			}
		}
		for _, expr := range n.LHS {
			if err := validateReservedExpr(expr, check); err != nil {
				return err
			}
		}
		for _, expr := range n.Values {
			if err := validateReservedExpr(expr, check); err != nil {
				return err
			}
		}
	case *ast.ReturnStmt:
		for _, expr := range n.Results {
			if err := validateReservedExpr(expr, check); err != nil {
				return err
			}
		}
	case *ast.CallExprStmt:
		return validateReservedExpr(n, check)
	case *ast.ExpressionStmt:
		return validateReservedExpr(n.X, check)
	case *ast.IncDecStmt:
		return validateReservedExpr(n.Operand, check)
	case *ast.IfStmt:
		if err := validateReservedExpr(n.Cond, check); err != nil {
			return err
		}
		if err := validateReservedStmt(asStmt(n.Body), check); err != nil {
			return err
		}
		return validateReservedStmt(asStmt(n.ElseBody), check)
	case *ast.ForStmt:
		if err := validateReservedStmt(asStmt(n.Init), check); err != nil {
			return err
		}
		if err := validateReservedStmt(asStmt(n.Update), check); err != nil {
			return err
		}
		if err := validateReservedExpr(n.Cond, check); err != nil {
			return err
		}
		return validateReservedStmt(asStmt(n.Body), check)
	case *ast.RangeStmt:
		if n.Define {
			if err := check(n.Key, "range variable"); err != nil {
				return err
			}
			if err := check(n.Value, "range variable"); err != nil {
				return err
			}
		}
		if err := validateReservedExpr(n.X, check); err != nil {
			return err
		}
		return validateReservedStmt(n.Body, check)
	case *ast.SwitchStmt:
		if err := validateReservedStmt(n.Init, check); err != nil {
			return err
		}
		if err := validateReservedStmt(n.Assign, check); err != nil {
			return err
		}
		if err := validateReservedExpr(n.Tag, check); err != nil {
			return err
		}
		return validateReservedStmt(n.Body, check)
	case *ast.CaseClause:
		for _, expr := range n.List {
			if err := validateReservedExpr(expr, check); err != nil {
				return err
			}
		}
		for _, child := range n.Body {
			if err := validateReservedStmt(child, check); err != nil {
				return err
			}
		}
	case *ast.TryStmt:
		if n.Catch != nil {
			if err := check(n.Catch.VarName, "catch variable"); err != nil {
				return err
			}
			if err := validateReservedStmt(n.Catch.Body, check); err != nil {
				return err
			}
		}
		if err := validateReservedStmt(asStmt(n.Body), check); err != nil {
			return err
		}
		return validateReservedStmt(asStmt(n.Finally), check)
	case *ast.DeferStmt:
		return validateReservedExpr(n.Call, check)
	case *ast.GoStmt:
		return validateReservedExpr(n.Call, check)
	}
	return nil
}

func validateReservedExpr(expr ast.Expr, check func(ast.Ident, string) error) error {
	if isNilInterface(expr) {
		return nil
	}
	switch n := expr.(type) {
	case nil:
		return nil
	case *ast.CallExprStmt:
		if err := validateReservedExpr(n.Func, check); err != nil {
			return err
		}
		for _, arg := range n.Args {
			if err := validateReservedExpr(arg, check); err != nil {
				return err
			}
		}
	case *ast.UnaryExpr:
		return validateReservedExpr(n.Operand, check)
	case *ast.BinaryExpr:
		if err := validateReservedExpr(n.Left, check); err != nil {
			return err
		}
		return validateReservedExpr(n.Right, check)
	case *ast.MemberExpr:
		return validateReservedExpr(n.Object, check)
	case *ast.IndexExpr:
		if err := validateReservedExpr(n.Object, check); err != nil {
			return err
		}
		return validateReservedExpr(n.Index, check)
	case *ast.SliceExpr:
		if err := validateReservedExpr(n.X, check); err != nil {
			return err
		}
		if err := validateReservedExpr(n.Low, check); err != nil {
			return err
		}
		return validateReservedExpr(n.High, check)
	case *ast.StarExpr:
		return validateReservedExpr(n.X, check)
	case *ast.TypeAssertExpr:
		return validateReservedExpr(n.X, check)
	case *ast.CompositeExpr:
		for _, elem := range n.Values {
			if err := validateReservedExpr(elem.Key, check); err != nil {
				return err
			}
			if err := validateReservedExpr(elem.Value, check); err != nil {
				return err
			}
		}
	case *ast.FuncLitExpr:
		for _, param := range n.Params {
			if err := check(param.Name, "parameter"); err != nil {
				return err
			}
		}
		return validateReservedStmt(n.Body, check)
	}
	return nil
}

func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func asStmt(node ast.Node) ast.Stmt {
	if st, ok := node.(ast.Stmt); ok {
		return st
	}
	return nil
}

func singleStmt(context string, items []ast.Stmt) (ast.Stmt, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return nil, fmt.Errorf("statement call template expanded to %d statements in %s, which accepts only one statement", len(items), context)
}

func copyRootSource(dst, src ast.Node) {
	if dst == nil || src == nil {
		return
	}
	dst.GetBase().Loc = src.GetBase().Loc
}
