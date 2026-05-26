package lowering

import (
	"errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/internal/miniident"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type builder struct {
	consts    map[string]string
	globals   map[string]struct{}
	functions map[string]struct{}
	err       error
}

// Error reports an AST node that cannot be represented in executable
// bytecode. It is returned before runtime construction so embedders can handle
// malformed hand-written ASTs without process-level panics.
type Error struct {
	Op       string
	NodeType string
	Meta     string
	ID       string
	File     string
	Line     int
	Col      int
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	loc := ""
	if e.File != "" || e.Line > 0 || e.Col > 0 {
		loc = fmt.Sprintf(" at %s:%d:%d", e.File, e.Line, e.Col)
	}
	detail := ""
	if e.Err != nil {
		detail = ": " + e.Err.Error()
	}
	return fmt.Sprintf("lowering %s failed for %s(meta=%s id=%s)%s%s", e.Op, e.NodeType, e.Meta, e.ID, loc, detail)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newBuilder(constants map[string]string, variables map[ast.Ident]ast.Expr, functions map[ast.Ident]*ast.FunctionStmt) *builder {
	b := &builder{
		consts:    cloneStringMap(constants),
		globals:   make(map[string]struct{}, len(variables)),
		functions: make(map[string]struct{}, len(functions)),
	}
	for ident := range variables {
		b.globals[string(ident)] = struct{}{}
	}
	for ident, fn := range functions {
		if fn != nil {
			b.functions[string(ident)] = struct{}{}
		}
	}
	return b
}

func PrepareProgram(program *ast.ProgramStmt) (*runtime.PreparedProgram, error) {
	if program == nil {
		return nil, errors.New("invalid program")
	}

	program.SyncTopLevelDeclVariables()
	groups, err := program.GlobalInitGroups()
	if err != nil {
		groups = program.DeclaredGlobalGroups()
	}
	order := make([]ast.Ident, 0, len(program.Variables))
	for _, group := range groups {
		order = append(order, group.Names...)
	}

	importAliases := make(map[string]string, len(program.Imports))
	for _, imp := range program.Imports {
		alias := imp.Alias
		if alias == "" {
			alias = importAliasFromPath(imp.Path)
		}
		importAliases[alias] = imp.Path
	}

	prepared := &runtime.PreparedProgram{
		Package:          program.Package,
		ImportAliases:    importAliases,
		Constants:        make(map[string]string, len(program.Constants)),
		NamedTypes:       make(map[string]runtime.RuntimeType, len(program.Types)),
		StructSchemas:    make(map[string]*runtime.RuntimeStructSpec, len(program.Structs)),
		InterfaceSchemas: make(map[string]*runtime.RuntimeInterfaceSpec, len(program.Interfaces)),
		Exports:          make(map[string]runtime.PreparedExport),
		GlobalInitOrder:  identSliceToStrings(order),
		GlobalInitGroups: make([]*runtime.PreparedGlobalInit, 0, len(groups)),
		Globals:          make(map[string]*runtime.PreparedGlobal, len(program.Variables)),
		Functions:        make(map[string]*runtime.PreparedFunction, len(program.Functions)),
	}
	for name, val := range program.Constants {
		prepared.Constants[name] = val
	}
	for ident, t := range program.Types {
		typeInfo, err := runtime.ParseRuntimeType(t)
		if err != nil {
			return nil, err
		}
		prepared.NamedTypes[string(ident)] = typeInfo
	}
	for ident, stmt := range program.Structs {
		spec, err := runtimeStructSpecFromStmt(stmt)
		if err != nil {
			return nil, err
		}
		if spec != nil {
			prepared.StructSchemas[string(ident)] = spec
		}
	}
	for ident, stmt := range program.Interfaces {
		spec, err := runtime.ParseRuntimeInterfaceSpec(stmt.Type)
		if err != nil {
			return nil, err
		}
		if spec != nil {
			prepared.InterfaceSchemas[string(ident)] = spec
		}
	}

	b := newBuilder(prepared.Constants, program.Variables, program.Functions)
	rootScope := b.newRootLoweringScope()
	for _, group := range groups {
		if len(group.Names) == 0 {
			continue
		}
		var planStmt ast.Stmt
		if group.Decl != nil {
			planStmt = group.Decl
		} else {
			bindings := make([]ast.VarBinding, 0, len(group.Names))
			for _, ident := range group.Names {
				kind := ast.GoMiniType(runtime.SpecAny)
				if expr := program.Variables[ident]; expr != nil && !expr.GetBase().Type.IsEmpty() {
					kind = expr.GetBase().Type
				}
				bindings = append(bindings, ast.VarBinding{Name: ident, Kind: kind})
			}
			planStmt = &ast.GenDeclStmt{BaseNode: ast.BaseNode{Meta: "decl"}, Bindings: bindings, Values: group.Values}
		}
		initPlan := b.tasksForStmtInScope(planStmt, nil, rootScope)
		if b.err != nil {
			return nil, b.err
		}
		prepared.GlobalInitGroups = append(prepared.GlobalInitGroups, &runtime.PreparedGlobalInit{
			Names:    identSliceToStrings(group.Names),
			InitPlan: initPlan,
		})
		if group.Decl != nil {
			for _, binding := range group.Decl.Bindings {
				if binding.Name == "" || binding.Name == "_" {
					continue
				}
				if _, ok := program.Variables[binding.Name]; !ok {
					continue
				}
				name := string(binding.Name)
				prepared.Globals[name] = &runtime.PreparedGlobal{
					Name:    name,
					Kind:    b.runtimeType(binding.Kind, group.Decl, "global declaration"),
					HasInit: len(group.Decl.Values) > 0,
				}
			}
			if b.err != nil {
				return nil, b.err
			}
			continue
		}
		for _, ident := range group.Names {
			expr := program.Variables[ident]
			kind := fallbackAnyType
			var initPlan []runtime.Task
			if expr != nil {
				if !expr.GetBase().Type.IsEmpty() {
					kind = b.runtimeType(expr.GetBase().Type, expr, "global initializer")
				}
				initPlan = b.tasksForExprInScope(expr, rootScope)
				if b.err != nil {
					return nil, b.err
				}
			}
			name := string(ident)
			prepared.Globals[name] = &runtime.PreparedGlobal{
				Name:     name,
				Kind:     kind,
				HasInit:  expr != nil,
				InitPlan: initPlan,
			}
		}
	}
	for ident := range program.Variables {
		name := string(ident)
		if _, ok := prepared.Globals[name]; !ok {
			prepared.Globals[name] = &runtime.PreparedGlobal{Name: name, Kind: fallbackAnyType}
		}
	}
	for ident, fn := range program.Functions {
		if fn == nil {
			continue
		}
		name := string(ident)
		fnScope := rootScope.childFunction()
		seenParams := make(map[string]struct{}, len(fn.Params))
		for _, p := range fn.Params {
			paramName := string(p.Name)
			if paramName != "" && paramName != "_" {
				if _, exists := seenParams[paramName]; exists {
					return nil, fmt.Errorf("parameter redeclared during lowering: %s", paramName)
				}
				seenParams[paramName] = struct{}{}
			}
			fnScope.declareParam(paramName)
		}
		sig, err := funcSigFromFunction(fn.FunctionType)
		if err != nil {
			return nil, err
		}
		prepared.Functions[name] = &runtime.PreparedFunction{
			Name:        name,
			Receiver:    runtime.TypeSpec(fn.ReceiverType),
			FunctionSig: sig,
			BodyTasks:   b.tasksForStmtInScope(fn.Body, nil, fnScope),
		}
		if b.err != nil {
			return nil, b.err
		}
	}
	mainStmts := make([]ast.Stmt, 0, len(program.Main))
	for _, stmt := range program.Main {
		decl, ok := stmt.(*ast.GenDeclStmt)
		if ok && isGlobalDecl(program, decl) {
			continue
		}
		mainStmts = append(mainStmts, stmt)
	}
	prepared.MainTasks = b.buildStmtPlanWithScope(mainStmts, rootScope)
	if b.err != nil {
		return nil, b.err
	}
	populatePreparedExports(prepared, program)
	if err := runtime.ValidatePreparedProgram(prepared); err != nil {
		return nil, err
	}
	return prepared, nil
}

func populatePreparedExports(prepared *runtime.PreparedProgram, program *ast.ProgramStmt) {
	if prepared == nil || program == nil {
		return
	}
	for name, val := range program.Constants {
		if !isExportedIdent(name) {
			continue
		}
		prepared.Exports[name] = runtime.PreparedExport{
			Name:       name,
			Kind:       runtime.PreparedExportConst,
			Type:       literalToVar(val).RuntimeType(),
			TargetName: name,
		}
	}
	for name, typ := range prepared.NamedTypes {
		if !isExportedIdent(name) {
			continue
		}
		prepared.Exports[name] = runtime.PreparedExport{
			Name:       name,
			Kind:       runtime.PreparedExportType,
			Type:       typ,
			TargetName: name,
		}
	}
	for ident, spec := range prepared.StructSchemas {
		if !isExportedIdent(ident) || spec == nil {
			continue
		}
		prepared.Exports[ident] = runtime.PreparedExport{
			Name:       ident,
			Kind:       runtime.PreparedExportStruct,
			Type:       spec.TypeInfo,
			TargetName: ident,
		}
	}
	for ident, spec := range prepared.InterfaceSchemas {
		if !isExportedIdent(ident) || spec == nil {
			continue
		}
		prepared.Exports[ident] = runtime.PreparedExport{
			Name:       ident,
			Kind:       runtime.PreparedExportInterface,
			Type:       spec.TypeInfo,
			TargetName: ident,
		}
	}
	for name, global := range prepared.Globals {
		if !isExportedIdent(name) || global == nil {
			continue
		}
		prepared.Exports[name] = runtime.PreparedExport{
			Name:       name,
			Kind:       runtime.PreparedExportGlobal,
			Type:       global.Kind,
			TargetName: name,
		}
	}
	for name, fn := range prepared.Functions {
		if !isExportedIdent(name) || fn == nil || fn.FunctionSig == nil {
			continue
		}
		typ, err := runtime.ParseRuntimeType(fn.FunctionSig.Spec)
		if err != nil || typ.IsEmpty() {
			typ, _ = runtime.ParseRuntimeType(runtime.TypeSpec(fn.FunctionSig.SignatureString()))
		}
		prepared.Exports[name] = runtime.PreparedExport{
			Name:       name,
			Kind:       runtime.PreparedExportFunc,
			Type:       typ,
			TargetName: name,
		}
	}
	if len(prepared.Exports) == 0 {
		prepared.Exports = nil
	}
}

func isExportedIdent(name string) bool {
	return miniident.IsExported(name)
}

func funcSigFromFunction(fn ast.FunctionType) (*runtime.RuntimeFuncSig, error) {
	sig, err := runtime.ParseRuntimeFuncSig(fn.MiniType())
	if err != nil {
		return nil, err
	}
	if sig == nil {
		return nil, errors.New("invalid function signature")
	}
	sig.ParamNames = sig.ParamNames[:0]
	for _, p := range fn.Params {
		sig.ParamNames = append(sig.ParamNames, string(p.Name))
	}
	return sig, nil
}

func runtimeStructSpecFromStmt(stmt *ast.StructStmt) (*runtime.RuntimeStructSpec, error) {
	if stmt == nil {
		return nil, nil
	}
	fields := make([]ast.StructMemberType, 0, len(stmt.FieldNames))
	for _, fieldName := range stmt.FieldNames {
		fields = append(fields, ast.StructMemberType{
			Name: string(fieldName),
			Type: stmt.Fields[fieldName],
		})
	}
	spec, err := runtime.ParseRuntimeStructSpec(string(stmt.Name), runtime.StructOwnershipVMValue, ast.CreateStructType(fields))
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, nil
	}
	spec.Spec = runtime.TypeSpec(stmt.Name)
	spec.TypeInfo.Raw = runtime.TypeSpec(stmt.Name)
	spec.TypeInfo.TypeID = runtime.CanonicalTypeID(string(stmt.Name))
	return spec, nil
}

func isGlobalDecl(program *ast.ProgramStmt, decl *ast.GenDeclStmt) bool {
	if program == nil || decl == nil {
		return false
	}
	for _, binding := range decl.Bindings {
		if binding.Name == "" || binding.Name == "_" {
			continue
		}
		if _, ok := program.Variables[binding.Name]; ok {
			return true
		}
	}
	return false
}

func identSliceToStrings(items []ast.Ident) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = string(item)
	}
	return out
}

func importAliasFromPath(path string) string {
	alias := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			alias = path[i+1:]
			break
		}
	}
	return alias
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
