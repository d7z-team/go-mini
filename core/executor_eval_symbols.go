package engine

import (
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (e *MiniExecutor) newCompilerConfigWithModuleSources(stagedSources, importedPrograms map[string]*ast.ProgramStmt) compiler.Config {
	schema := e.ExportedSchema()
	e.mu.RLock()
	surfaceSchema := runtime.CloneFFISurfaceSchema(e.surfaceSchema)
	moduleHashes := cloneSurfaceLibraryHashes(e.libraryHashes)
	e.mu.RUnlock()
	return compiler.Config{
		ModuleLoader:            e.moduleASTLoaderWithStaged(stagedSources, importedPrograms),
		Surface:                 surfaceSchema,
		FuncSchemas:             schema.Funcs,
		RegisteredFuncs:         schema.RegisteredFuncs,
		RegisteredFuncMethodIDs: schema.RegisteredFuncMethodIDs,
		ValueSchemas:            schema.Values,
		StructSchemas:           schema.Structs,
		InterfaceSchemas:        schema.Interfaces,
		Constants:               e.GetExportedConstants(),
		ModuleHashes:            moduleHashes,
		MaxTypeDepth:            e.MaxTypeDepth,
		Templates:               e.templateRegistrySnapshot(),
	}
}

func (e *MiniExecutor) newEvalCompiler(prepared *runtime.PreparedProgram) *compiler.Compiler {
	cfg := e.newCompilerConfigWithModuleSources(nil, nil)
	addPreparedSymbolsToCompilerConfig(&cfg, prepared)
	return compiler.New(cfg)
}

func addPreparedSymbolsToCompilerConfig(cfg *compiler.Config, prepared *runtime.PreparedProgram) {
	if cfg == nil || prepared == nil {
		return
	}
	ensureCompilerConfigMaps(cfg)
	addPreparedProgramSymbols(cfg, prepared, "")
	for path, module := range prepared.Modules {
		addPreparedProgramSymbols(cfg, module, strings.TrimSpace(path))
	}
}

func ensureCompilerConfigMaps(cfg *compiler.Config) {
	if cfg.FuncSchemas == nil {
		cfg.FuncSchemas = make(map[ast.Ident]*runtime.RuntimeFuncSig)
	}
	if cfg.RegisteredFuncs == nil {
		cfg.RegisteredFuncs = make(map[ast.Ident]bool)
	}
	if cfg.RegisteredFuncMethodIDs == nil {
		cfg.RegisteredFuncMethodIDs = make(map[ast.Ident]uint32)
	}
	if cfg.ValueSchemas == nil {
		cfg.ValueSchemas = make(map[ast.Ident]*runtime.ValueSpec)
	}
	if cfg.StructSchemas == nil {
		cfg.StructSchemas = make(map[ast.Ident]*runtime.RuntimeStructSpec)
	}
	if cfg.InterfaceSchemas == nil {
		cfg.InterfaceSchemas = make(map[ast.Ident]*runtime.RuntimeInterfaceSpec)
	}
	if cfg.Constants == nil {
		cfg.Constants = make(map[string]runtime.FFIConstValue)
	}
}

func addPreparedProgramSymbols(cfg *compiler.Config, prepared *runtime.PreparedProgram, modulePath string) {
	if prepared == nil {
		return
	}
	structs := make(map[ast.Ident]*runtime.RuntimeStructSpec, len(prepared.StructSchemas))
	for name, spec := range prepared.StructSchemas {
		key := qualifiedPreparedName(modulePath, name)
		structs[ast.Ident(key)] = cloneQualifiedStructSpec(modulePath, name, spec)
	}
	attachPreparedMethods(structs, prepared.Functions, modulePath)
	for name, spec := range structs {
		cfg.StructSchemas[name] = spec
	}
	for name, spec := range prepared.InterfaceSchemas {
		key := qualifiedPreparedName(modulePath, name)
		if cloned := cloneQualifiedInterfaceSpec(modulePath, name, spec); cloned != nil {
			cfg.InterfaceSchemas[ast.Ident(key)] = cloned
		}
	}
	for name, value := range prepared.Constants {
		cfg.Constants[qualifiedPreparedName(modulePath, name)] = value
	}
	for name, global := range prepared.Globals {
		if global == nil {
			continue
		}
		cfg.ValueSchemas[ast.Ident(qualifiedPreparedName(modulePath, name))] = &runtime.ValueSpec{Type: global.Kind}
	}
	for name, fn := range prepared.Functions {
		if fn == nil || fn.FunctionSig == nil {
			continue
		}
		cfg.FuncSchemas[ast.Ident(qualifiedPreparedName(modulePath, name))] = runtime.CloneRuntimeFuncSig(fn.FunctionSig)
	}
	if modulePath == "" {
		return
	}
	for exportName, export := range prepared.Exports {
		target := export.TargetName
		if strings.TrimSpace(target) == "" {
			target = export.Name
		}
		fullName := modulePath + "." + exportName
		switch export.Kind {
		case runtime.PreparedExportFunc:
			if fn := prepared.Functions[target]; fn != nil && fn.FunctionSig != nil {
				cfg.FuncSchemas[ast.Ident(fullName)] = runtime.CloneRuntimeFuncSig(fn.FunctionSig)
			}
		case runtime.PreparedExportGlobal:
			if global := prepared.Globals[target]; global != nil {
				cfg.ValueSchemas[ast.Ident(fullName)] = &runtime.ValueSpec{Type: global.Kind}
			}
		case runtime.PreparedExportConst:
			if value, ok := prepared.Constants[target]; ok {
				cfg.Constants[fullName] = value
			}
		case runtime.PreparedExportStruct:
			if spec := prepared.StructSchemas[target]; spec != nil {
				cfg.StructSchemas[ast.Ident(fullName)] = cloneQualifiedStructSpec(modulePath, exportName, spec)
			}
		case runtime.PreparedExportInterface:
			if spec := prepared.InterfaceSchemas[target]; spec != nil {
				cfg.InterfaceSchemas[ast.Ident(fullName)] = cloneQualifiedInterfaceSpec(modulePath, exportName, spec)
			}
		}
	}
}

func attachPreparedMethods(structs map[ast.Ident]*runtime.RuntimeStructSpec, funcs map[string]*runtime.PreparedFunction, modulePath string) {
	for name, fn := range funcs {
		if fn == nil || fn.FunctionSig == nil || fn.Receiver.IsEmpty() {
			continue
		}
		receiverName := strings.TrimSpace(fn.Receiver.String())
		if receiverName == "" {
			continue
		}
		if modulePath != "" && !strings.Contains(receiverName, ".") {
			receiverName = modulePath + "." + receiverName
		}
		spec := structs[ast.Ident(receiverName)]
		if spec == nil {
			short := strings.TrimPrefix(receiverName, modulePath+".")
			spec = structs[ast.Ident(short)]
		}
		if spec == nil {
			continue
		}
		methodName := name
		if idx := strings.LastIndex(methodName, "."); idx >= 0 {
			methodName = methodName[idx+1:]
		}
		if methodName == "" {
			continue
		}
		if spec.ByMethod == nil {
			spec.ByMethod = make(map[string]*runtime.RuntimeFuncSig)
		}
		if _, exists := spec.ByMethod[methodName]; exists {
			continue
		}
		cloned := runtime.CloneRuntimeFuncSig(fn.FunctionSig)
		spec.Methods = append(spec.Methods, runtime.RuntimeStructMethod{Name: methodName, Spec: cloned})
		spec.ByMethod[methodName] = cloned
	}
}

func qualifiedPreparedName(modulePath, name string) string {
	name = strings.TrimSpace(name)
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" || name == "" || strings.Contains(name, ".") {
		return name
	}
	return modulePath + "." + name
}

func cloneQualifiedStructSpec(modulePath, name string, spec *runtime.RuntimeStructSpec) *runtime.RuntimeStructSpec {
	cloned := runtime.CloneRuntimeStructSpec(spec)
	if cloned == nil {
		return nil
	}
	qualified := qualifiedPreparedName(modulePath, name)
	if qualified == "" {
		return cloned
	}
	cloned.Name = qualified
	cloned.TypeID = runtime.CanonicalTypeID(qualified)
	cloned.Spec = runtime.TypeSpec(qualified)
	cloned.TypeInfo.Raw = runtime.TypeSpec(qualified)
	cloned.TypeInfo.TypeID = cloned.TypeID
	cloned.TypeInfo.Fields = append([]runtime.RuntimeStructField(nil), cloned.Fields...)
	return cloned
}

func cloneQualifiedInterfaceSpec(modulePath, name string, spec *runtime.RuntimeInterfaceSpec) *runtime.RuntimeInterfaceSpec {
	cloned := runtime.CloneRuntimeInterfaceSpec(spec)
	if cloned == nil {
		return nil
	}
	qualified := qualifiedPreparedName(modulePath, name)
	if qualified == "" {
		return cloned
	}
	cloned.TypeID = runtime.CanonicalTypeID(qualified)
	cloned.TypeInfo.Raw = runtime.TypeSpec(qualified)
	cloned.TypeInfo.TypeID = cloned.TypeID
	return cloned
}
