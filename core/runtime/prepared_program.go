package runtime

type PreparedProgram struct {
	Package              string                           `json:"package,omitempty"`
	ImportAliases        map[string]string                `json:"import_aliases,omitempty"`
	Constants            map[string]FFIConstValue         `json:"constants,omitempty"`
	ConstantTypes        map[string]RuntimeType           `json:"constant_types,omitempty"`
	NamedTypes           map[string]RuntimeType           `json:"named_types,omitempty"`
	StructSchemas        map[string]*RuntimeStructSpec    `json:"struct_schemas,omitempty"`
	InterfaceSchemas     map[string]*RuntimeInterfaceSpec `json:"interface_schemas,omitempty"`
	Exports              map[string]PreparedExport        `json:"exports,omitempty"`
	Modules              map[string]*PreparedProgram      `json:"modules,omitempty"`
	ModuleHashes         map[string]string                `json:"module_hashes,omitempty"`
	ExternalRequirements []ExternalRequirement            `json:"external_requirements,omitempty"`

	GlobalInitOrder  []string                     `json:"global_init_order"`
	GlobalInitGroups []*PreparedGlobalInit        `json:"global_init_groups,omitempty"`
	Globals          map[string]*PreparedGlobal   `json:"globals"`
	Functions        map[string]*PreparedFunction `json:"functions"`
	MainTasks        []Task                       `json:"main_tasks"`
}

type PreparedExportKind string

const (
	PreparedExportFunc      PreparedExportKind = "func"
	PreparedExportGlobal    PreparedExportKind = "global"
	PreparedExportConst     PreparedExportKind = "const"
	PreparedExportType      PreparedExportKind = "type"
	PreparedExportStruct    PreparedExportKind = "struct"
	PreparedExportInterface PreparedExportKind = "interface"
)

type PreparedExport struct {
	Name       string             `json:"name"`
	Kind       PreparedExportKind `json:"kind"`
	Type       RuntimeType        `json:"type,omitempty"`
	TargetName string             `json:"target_name,omitempty"`
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
	Receiver    TypeSpec        `json:"receiver,omitempty"`
	FunctionSig *RuntimeFuncSig `json:"function_sig,omitempty"`
	BodyTasks   []Task          `json:"body_tasks,omitempty"`
}

func clonePreparedProgram(plan *PreparedProgram) *PreparedProgram {
	if plan == nil {
		return nil
	}

	cloned := &PreparedProgram{
		Package:              plan.Package,
		ImportAliases:        cloneStringMap(plan.ImportAliases),
		Constants:            cloneFFIConstValueMap(plan.Constants),
		ConstantTypes:        cloneRuntimeTypeMap(plan.ConstantTypes),
		NamedTypes:           cloneRuntimeTypeMap(plan.NamedTypes),
		StructSchemas:        cloneRuntimeStructSpecMap(plan.StructSchemas),
		InterfaceSchemas:     cloneRuntimeInterfaceSpecMap(plan.InterfaceSchemas),
		Exports:              clonePreparedExportMap(plan.Exports),
		Modules:              clonePreparedProgramMap(plan.Modules),
		ModuleHashes:         cloneStringMap(plan.ModuleHashes),
		ExternalRequirements: append([]ExternalRequirement(nil), plan.ExternalRequirements...),
		GlobalInitOrder:      append([]string(nil), plan.GlobalInitOrder...),
		GlobalInitGroups:     make([]*PreparedGlobalInit, 0, len(plan.GlobalInitGroups)),
		Globals:              make(map[string]*PreparedGlobal, len(plan.Globals)),
		Functions:            make(map[string]*PreparedFunction, len(plan.Functions)),
		MainTasks:            cloneTasks(plan.MainTasks),
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
			Receiver:    fn.Receiver,
			FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
			BodyTasks:   cloneTasks(fn.BodyTasks),
		}
	}

	return cloned
}

func clonePreparedExportMap(in map[string]PreparedExport) map[string]PreparedExport {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]PreparedExport, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func clonePreparedProgramMap(in map[string]*PreparedProgram) map[string]*PreparedProgram {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*PreparedProgram, len(in))
	for path, plan := range in {
		out[path] = clonePreparedProgram(plan)
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
