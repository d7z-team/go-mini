package runtime

import (
	"errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
)

type PreparedProgram struct {
	Package          string                           `json:"package,omitempty"`
	ImportAliases    map[string]string                `json:"import_aliases,omitempty"`
	Constants        map[string]string                `json:"constants,omitempty"`
	NamedTypes       map[string]RuntimeType           `json:"named_types,omitempty"`
	StructSchemas    map[string]*RuntimeStructSpec    `json:"struct_schemas,omitempty"`
	InterfaceSchemas map[string]*RuntimeInterfaceSpec `json:"interface_schemas,omitempty"`

	GlobalInitOrder  []string                     `json:"global_init_order"`
	GlobalInitGroups []*PreparedGlobalInit        `json:"global_init_groups,omitempty"`
	Globals          map[string]*PreparedGlobal   `json:"globals"`
	Functions        map[string]*PreparedFunction `json:"functions"`
	MainTasks        []Task                       `json:"main_tasks"`
}

type PreparedGlobal struct {
	Name     string      `json:"name"`
	Kind     RuntimeType `json:"kind"`
	HasInit  bool        `json:"has_init"`
	InitPlan []Task      `json:"init_plan,omitempty"`
}

type PreparedGlobalInit struct {
	Names    []string `json:"names"`
	InitPlan []Task   `json:"init_plan,omitempty"`
}

type PreparedFunction struct {
	Name        string          `json:"name"`
	FunctionSig *RuntimeFuncSig `json:"function_sig,omitempty"`
	BodyTasks   []Task          `json:"body_tasks,omitempty"`
}

func PrepareProgram(program *ast.ProgramStmt) (*PreparedProgram, error) {
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

	prepared := &PreparedProgram{
		Package:          program.Package,
		ImportAliases:    importAliases,
		Constants:        make(map[string]string, len(program.Constants)),
		NamedTypes:       make(map[string]RuntimeType, len(program.Types)),
		StructSchemas:    make(map[string]*RuntimeStructSpec, len(program.Structs)),
		InterfaceSchemas: make(map[string]*RuntimeInterfaceSpec, len(program.Interfaces)),
		GlobalInitOrder:  identSliceToStrings(order),
		GlobalInitGroups: make([]*PreparedGlobalInit, 0, len(groups)),
		Globals:          make(map[string]*PreparedGlobal, len(program.Variables)),
		Functions:        make(map[string]*PreparedFunction, len(program.Functions)),
	}
	for name, val := range program.Constants {
		prepared.Constants[name] = val
	}
	for ident, t := range program.Types {
		typeInfo, err := ParseRuntimeType(t)
		if err != nil {
			return nil, err
		}
		prepared.NamedTypes[string(ident)] = typeInfo
	}
	for ident, stmt := range program.Structs {
		spec := runtimeStructSpecFromStmt(stmt)
		if spec != nil {
			prepared.StructSchemas[string(ident)] = spec
		}
	}
	for ident, stmt := range program.Interfaces {
		spec, err := ParseRuntimeInterfaceSpec(stmt.Type)
		if err != nil {
			return nil, err
		}
		if spec != nil {
			prepared.InterfaceSchemas[string(ident)] = spec
		}
	}

	exec := &Executor{
		consts:    cloneStringMap(prepared.Constants),
		globals:   make(map[string]*RuntimeGlobal, len(program.Variables)),
		functions: make(map[string]*RuntimeFunction, len(program.Functions)),
	}
	for ident := range program.Variables {
		name := string(ident)
		exec.globals[name] = &RuntimeGlobal{Name: name}
	}
	for ident, fn := range program.Functions {
		if fn == nil {
			continue
		}
		name := string(ident)
		exec.functions[name] = &RuntimeFunction{Name: name}
	}

	rootScope := exec.newRootLoweringScope()
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
				kind := ast.TypeAny
				if expr := program.Variables[ident]; expr != nil && !expr.GetBase().Type.IsEmpty() {
					kind = expr.GetBase().Type
				}
				bindings = append(bindings, ast.VarBinding{Name: ident, Kind: kind})
			}
			planStmt = &ast.GenDeclStmt{BaseNode: ast.BaseNode{Meta: "decl"}, Bindings: bindings, Values: group.Values}
		}
		initPlan := exec.tasksForStmtInScope(planStmt, nil, rootScope)
		prepared.GlobalInitGroups = append(prepared.GlobalInitGroups, &PreparedGlobalInit{
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
				prepared.Globals[name] = &PreparedGlobal{
					Name:    name,
					Kind:    MustParseRuntimeType(binding.Kind),
					HasInit: len(group.Decl.Values) > 0,
				}
			}
			continue
		}
		for _, ident := range group.Names {
			expr := program.Variables[ident]
			kind := MustParseRuntimeType(ast.TypeAny)
			var initPlan []Task
			if expr != nil {
				if !expr.GetBase().Type.IsEmpty() {
					kind = MustParseRuntimeType(expr.GetBase().Type)
				}
				initPlan = exec.tasksForExprInScope(expr, rootScope)
			}
			name := string(ident)
			prepared.Globals[name] = &PreparedGlobal{
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
			prepared.Globals[name] = &PreparedGlobal{Name: name, Kind: MustParseRuntimeType(ast.TypeAny)}
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
		prepared.Functions[name] = &PreparedFunction{
			Name:        name,
			FunctionSig: MustRuntimeFuncSigFromFunction(fn.FunctionType),
			BodyTasks:   exec.tasksForStmtInScope(fn.Body, nil, fnScope),
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
	prepared.MainTasks = exec.buildStmtPlanWithScope(mainStmts, rootScope)
	return prepared, nil
}

func clonePreparedProgram(plan *PreparedProgram) *PreparedProgram {
	if plan == nil {
		return nil
	}

	cloned := &PreparedProgram{
		Package:          plan.Package,
		ImportAliases:    cloneStringMap(plan.ImportAliases),
		Constants:        cloneStringMap(plan.Constants),
		NamedTypes:       cloneRuntimeTypeMap(plan.NamedTypes),
		StructSchemas:    cloneRuntimeStructSpecMap(plan.StructSchemas),
		InterfaceSchemas: cloneRuntimeInterfaceSpecMap(plan.InterfaceSchemas),
		GlobalInitOrder:  append([]string(nil), plan.GlobalInitOrder...),
		GlobalInitGroups: make([]*PreparedGlobalInit, 0, len(plan.GlobalInitGroups)),
		Globals:          make(map[string]*PreparedGlobal, len(plan.Globals)),
		Functions:        make(map[string]*PreparedFunction, len(plan.Functions)),
		MainTasks:        cloneTasks(plan.MainTasks),
	}

	for _, group := range plan.GlobalInitGroups {
		if group == nil {
			cloned.GlobalInitGroups = append(cloned.GlobalInitGroups, nil)
			continue
		}
		cloned.GlobalInitGroups = append(cloned.GlobalInitGroups, &PreparedGlobalInit{
			Names:    append([]string(nil), group.Names...),
			InitPlan: cloneTasks(group.InitPlan),
		})
	}
	for name, global := range plan.Globals {
		if global == nil {
			cloned.Globals[name] = nil
			continue
		}
		cloned.Globals[name] = &PreparedGlobal{
			Name:     global.Name,
			Kind:     global.Kind,
			HasInit:  global.HasInit,
			InitPlan: cloneTasks(global.InitPlan),
		}
	}
	for name, fn := range plan.Functions {
		if fn == nil {
			cloned.Functions[name] = nil
			continue
		}
		cloned.Functions[name] = &PreparedFunction{
			Name:        fn.Name,
			FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
			BodyTasks:   cloneTasks(fn.BodyTasks),
		}
	}

	return cloned
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

func cloneRuntimeTypeMap(in map[string]RuntimeType) map[string]RuntimeType {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]RuntimeType, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneRuntimeStructSpecMap(in map[string]*RuntimeStructSpec) map[string]*RuntimeStructSpec {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*RuntimeStructSpec, len(in))
	for k, v := range in {
		out[k] = CloneRuntimeStructSpec(v)
	}
	return out
}

func cloneRuntimeInterfaceSpecMap(in map[string]*RuntimeInterfaceSpec) map[string]*RuntimeInterfaceSpec {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*RuntimeInterfaceSpec, len(in))
	for k, v := range in {
		out[k] = CloneRuntimeInterfaceSpec(v)
	}
	return out
}
