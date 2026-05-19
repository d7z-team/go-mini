package runtime

import (
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
)

type PreparedProgram struct {
	Package          string                           `json:"package,omitempty"`
	ImportAliases    map[string]string                `json:"import_aliases,omitempty"`
	Constants        map[string]string                `json:"constants,omitempty"`
	NamedTypes       map[string]RuntimeType           `json:"named_types,omitempty"`
	StructSchemas    map[string]*RuntimeStructSpec    `json:"struct_schemas,omitempty"`
	InterfaceSchemas map[string]*RuntimeInterfaceSpec `json:"interface_schemas,omitempty"`

	GlobalInitOrder []string                     `json:"global_init_order"`
	Globals         map[string]*PreparedGlobal   `json:"globals"`
	Functions       map[string]*PreparedFunction `json:"functions"`
	MainTasks       []Task                       `json:"main_tasks"`
}

type PreparedGlobal struct {
	Name     string      `json:"name"`
	Kind     RuntimeType `json:"kind"`
	HasInit  bool        `json:"has_init"`
	InitPlan []Task      `json:"init_plan,omitempty"`
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

	order, err := program.GlobalInitOrder()
	if err != nil {
		order = program.DeclaredGlobalOrder()
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
	globalKinds := make(map[string]RuntimeType, len(program.Variables))
	for _, stmt := range program.Main {
		if decl, ok := stmt.(*ast.GenDeclStmt); ok {
			globalKinds[string(decl.Name)] = MustParseRuntimeType(decl.Kind)
		}
	}
	for ident, expr := range program.Variables {
		name := string(ident)
		item := &PreparedGlobal{
			Name:    name,
			HasInit: expr != nil,
		}
		if kind, ok := globalKinds[name]; ok {
			item.Kind = kind
		} else if expr != nil {
			item.Kind = MustParseRuntimeType(expr.GetBase().Type)
		}
		if expr != nil {
			item.InitPlan = exec.tasksForExprInScope(expr, rootScope)
		}
		prepared.Globals[name] = item
	}
	for ident, fn := range program.Functions {
		if fn == nil {
			continue
		}
		name := string(ident)
		fnScope := rootScope.childFunction()
		for _, p := range fn.Params {
			fnScope.declare(string(p.Name))
		}
		prepared.Functions[name] = &PreparedFunction{
			Name:        name,
			FunctionSig: MustRuntimeFuncSigFromFunction(fn.FunctionType),
			BodyTasks:   exec.tasksForStmtInScope(fn.Body, nil, fnScope),
		}
	}
	prepared.MainTasks = exec.buildStmtPlanWithScope(program.Main, rootScope)
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
		Globals:          make(map[string]*PreparedGlobal, len(plan.Globals)),
		Functions:        make(map[string]*PreparedFunction, len(plan.Functions)),
		MainTasks:        cloneTasks(plan.MainTasks),
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
			FunctionSig: cloneRuntimeFuncSig(fn.FunctionSig),
			BodyTasks:   cloneTasks(fn.BodyTasks),
		}
	}

	return cloned
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
		out[k] = cloneRuntimeStructSpec(v)
	}
	return out
}

func cloneRuntimeInterfaceSpecMap(in map[string]*RuntimeInterfaceSpec) map[string]*RuntimeInterfaceSpec {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*RuntimeInterfaceSpec, len(in))
	for k, v := range in {
		out[k] = cloneRuntimeInterfaceSpec(v)
	}
	return out
}
