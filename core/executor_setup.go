package engine

import (
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	coreffilib "gopkg.d7z.net/go-mini/core/ffilib"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		routes:         make(map[string]runtime.FFIRoute),
		constants:      make(map[string]string),
		registry:       ffigo.NewHandleRegistry(),
		moduleSources:  make(map[string]*ast.ProgramStmt),
		modules:        make(map[string]*runtime.PreparedProgram),
		funcSchemas:    make(map[ast.Ident]*runtime.RuntimeFuncSig),
		valueSchemas:   make(map[ast.Ident]*runtime.ValueSpec),
		packageValues:  make(map[string]*runtime.BoundPackageValue),
		surfaceSchema:  runtime.NewFFISurfaceSchema(),
		boundSurface:   runtime.NewBoundFFISurface(runtime.NewFFISurfaceSchema()),
		structsMeta:    make(map[ast.Ident]*runtime.RuntimeStructSpec),
		interfacesMeta: make(map[ast.Ident]*runtime.RuntimeInterfaceSpec),
		templates:      calltemplate.NewRegistry(),
		MaxTypeDepth:   256,
	}

	// 默认注册 panic 签名以便通过验证
	res.mustAddFuncSchemaLocked("panic", runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString))
	res.mustAddFuncSchemaLocked("recover", runtime.MustRuntimeFuncSig(runtime.SpecAny, false))
	res.mustAddFuncSchemaLocked("String", runtime.MustRuntimeFuncSig(runtime.SpecString, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("TypeBytes", runtime.MustRuntimeFuncSig(runtime.SpecBytes, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("len", runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("cap", runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("make", runtime.MustRuntimeFuncSig(runtime.SpecAny, true, runtime.SpecString, runtime.SpecInt64))
	res.mustAddFuncSchemaLocked("new", runtime.MustRuntimeFuncSig(runtime.SpecAny, false, runtime.SpecString))
	res.mustAddFuncSchemaLocked("append", runtime.MustRuntimeFuncSig(runtime.SpecAny, true, runtime.SpecAny, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("delete", runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("Int64", runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("Float64", runtime.MustRuntimeFuncSig(runtime.SpecFloat64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("require", runtime.MustRuntimeFuncSig(runtime.SpecModule, false, runtime.SpecString))

	if err := res.UseSurface(coreffilib.Surface()); err != nil {
		panic(err)
	}

	return res
}

func (e *MiniExecutor) UseSurface(bundle *surface.Bundle) error {
	if bundle == nil {
		return nil
	}
	if bundle.Err != nil {
		return bundle.Err
	}
	if err := runtime.CheckPublicFFISurfaceSchema(bundle.Schema); err != nil {
		return err
	}
	var bound *runtime.BoundFFISurface
	if bundle.Bind != nil {
		var err error
		bound, err = bundle.Bind(runtime.FFIBindContext{Registry: e.registry})
		if err != nil {
			return err
		}
	}
	if bundle.Schema != nil || bound != nil || len(bundle.Templates) > 0 {
		e.mu.Lock()
		defer e.mu.Unlock()

		var nextTemplates *calltemplate.Registry
		if len(bundle.Templates) > 0 {
			incomingSymbolExists := func(name string) bool {
				if bound != nil {
					if _, ok := bound.Routes[name]; ok {
						return true
					}
					if _, ok := bound.PackageValues[name]; ok {
						return true
					}
					if _, ok := bound.Consts[name]; ok {
						return true
					}
					if _, ok := bound.Structs[name]; ok {
						return true
					}
					if _, ok := bound.Interfaces[name]; ok {
						return true
					}
				}
				if bundle.Schema == nil {
					return false
				}
				if _, ok := bundle.Schema.Types[name]; ok {
					return true
				}
				pkgPath, memberName := runtime.SplitExternalName(name)
				if pkgPath == "" || memberName == "" {
					return false
				}
				if pkg := bundle.Schema.Packages[pkgPath]; pkg != nil {
					_, ok := pkg.Members[memberName]
					return ok
				}
				return false
			}
			if e.templates != nil {
				nextTemplates = e.templates.Clone()
			}
			if nextTemplates == nil {
				nextTemplates = calltemplate.NewRegistry()
			}
			for _, tpl := range bundle.Templates {
				if err := nextTemplates.Register(tpl); err != nil {
					return err
				}
			}
			for name, registered := range nextTemplates.Globals() {
				if e.globalSymbolExistsLocked(name) || incomingSymbolExists(name) {
					return fmt.Errorf("global call template %s conflicts with existing symbol %s", registered.ID, name)
				}
			}
		}
		nextSchema := runtime.CloneFFISurfaceSchema(e.surfaceSchema)
		if nextSchema == nil {
			nextSchema = runtime.NewFFISurfaceSchema()
		}
		if err := nextSchema.Merge(bundle.Schema); err != nil {
			return err
		}
		nextBound := runtime.NewBoundFFISurface(nextSchema)
		if e.boundSurface != nil {
			if err := nextBound.Merge(e.boundSurface); err != nil {
				return err
			}
		}
		if bound != nil {
			if err := nextBound.Merge(bound); err != nil {
				return err
			}
			if err := e.validateBoundSurfaceLocked(bound); err != nil {
				return err
			}
			e.applyBoundSurfaceChangesLocked(bound)
		}
		e.surfaceSchema = nextSchema
		e.boundSurface = nextBound
		if nextTemplates != nil {
			e.templates = nextTemplates
		}
	}
	return nil
}

func (e *MiniExecutor) moduleASTLoader() func(path string) (*ast.ProgramStmt, error) {
	return func(path string) (*ast.ProgramStmt, error) {
		e.mu.RLock()
		astNode, ok := e.moduleSources[path]
		loader := e.astModuleLoader
		e.mu.RUnlock()
		if ok {
			return astNode, nil
		}
		if loader != nil {
			return loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}
}

func (e *MiniExecutor) modulePlanLoader() func(path string) (*runtime.PreparedProgram, error) {
	return func(path string) (*runtime.PreparedProgram, error) {
		e.mu.RLock()
		if prepared, ok := e.modules[path]; ok && prepared != nil {
			e.mu.RUnlock()
			return prepared, nil
		}
		e.mu.RUnlock()
		return nil, fmt.Errorf("%w: %s", runtime.ErrModuleNotFound, path)
	}
}

func (e *MiniExecutor) applyExecutorConfig(executor *runtime.Executor) error {
	if executor == nil {
		return nil
	}
	executor.ModulePlanLoader = e.modulePlanLoader()

	e.mu.RLock()
	defer e.mu.RUnlock()
	for name, route := range e.routes {
		if err := executor.TryRegisterRoute(name, route); err != nil {
			return err
		}
	}
	for name, spec := range e.structsMeta {
		if err := executor.TryRegisterStructSchema(string(name), spec); err != nil {
			return err
		}
	}
	for name, spec := range e.interfacesMeta {
		if err := executor.TryRegisterInterfaceSchema(string(name), spec); err != nil {
			return err
		}
	}
	for name, value := range e.packageValues {
		if value == nil {
			continue
		}
		if err := executor.TryRegisterPackageValue(name, value.Spec, value.Value); err != nil {
			return err
		}
	}
	for name, val := range e.constants {
		executor.RegisterConstant(name, val)
	}
	return nil
}

func (e *MiniExecutor) newCompiler() *compiler.Compiler {
	schema := e.ExportedSchema()
	e.mu.RLock()
	surfaceSchema := runtime.CloneFFISurfaceSchema(e.surfaceSchema)
	e.mu.RUnlock()
	return compiler.New(compiler.Config{
		ModuleLoader:            e.moduleASTLoader(),
		Surface:                 surfaceSchema,
		FuncSchemas:             schema.Funcs,
		RegisteredFuncs:         schema.RegisteredFuncs,
		RegisteredFuncMethodIDs: schema.RegisteredFuncMethodIDs,
		ValueSchemas:            schema.Values,
		StructSchemas:           schema.Structs,
		InterfaceSchemas:        schema.Interfaces,
		Constants:               e.GetExportedConstants(),
		MaxTypeDepth:            e.MaxTypeDepth,
		Templates:               e.templateRegistrySnapshot(),
	})
}

func newMiniAstError(err error, semanticCtx *ast.SemanticContext, node ast.Node) error {
	var logs []ast.Logs
	if semanticCtx != nil {
		logs = semanticCtx.Logs()
	}
	return &ast.MiniAstError{Err: err, Logs: logs, Node: node}
}

func compiledProgramNode(compiled *compiler.Artifact) *ast.ProgramStmt {
	if compiled == nil {
		return nil
	}
	return compiled.Program
}

func (e *MiniExecutor) prepareArtifactModules(compiled *compiler.Artifact) error {
	if compiled == nil || compiled.Program == nil {
		return nil
	}
	if len(compiled.ImportedPrograms) > 0 {
		e.mu.Lock()
		for _, imp := range compiled.Program.Imports {
			path := strings.TrimSpace(imp.Path)
			if path == "" {
				continue
			}
			if prog := compiled.ImportedPrograms[path]; prog != nil {
				e.moduleSources[path] = prog
			}
		}
		e.mu.Unlock()
	}
	return e.compileImportedModules(compiled.Program, compiled.ImportedPrograms, map[string]bool{})
}

func (e *MiniExecutor) compileImportedModules(program *ast.ProgramStmt, imported map[string]*ast.ProgramStmt, visiting map[string]bool) error {
	for _, imp := range program.Imports {
		path := strings.TrimSpace(imp.Path)
		if path == "" {
			continue
		}

		e.mu.RLock()
		prepared := e.modules[path]
		prog := e.moduleSources[path]
		e.mu.RUnlock()
		if prepared != nil {
			continue
		}
		if prog == nil && imported != nil {
			prog = imported[path]
		}
		if prog == nil {
			continue
		}
		if visiting[path] {
			return fmt.Errorf("circular module dependency while preparing %s", path)
		}

		visiting[path] = true
		compiled, _, err := e.newCompiler().CompileProgram(path, "", prog, false)
		if err != nil {
			delete(visiting, path)
			return fmt.Errorf("compile module %s: %w", path, err)
		}
		if compiled == nil || compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
			delete(visiting, path)
			return fmt.Errorf("module %s did not produce executable bytecode", path)
		}

		e.mu.Lock()
		e.modules[path] = compiled.Bytecode.Executable
		if prog != nil {
			e.moduleSources[path] = prog
		}
		e.mu.Unlock()

		if err := e.compileImportedModules(compiled.Program, compiled.ImportedPrograms, visiting); err != nil {
			delete(visiting, path)
			return err
		}
		delete(visiting, path)
	}
	return nil
}

func newEmptyRuntimeExecutor() (*runtime.Executor, error) {
	return runtime.NewExecutorFromPrepared(&runtime.PreparedProgram{
		Globals:   map[string]*runtime.PreparedGlobal{},
		Functions: map[string]*runtime.PreparedFunction{},
		MainTasks: []runtime.Task{},
	})
}

func (e *MiniExecutor) SetModuleLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.astModuleLoader = loader
}

// RegisterModule 注册一个预编译的模块，使得脚本可以通过 import 直接引用
func (e *MiniExecutor) RegisterModule(path string, prog *MiniProgram) {
	var prepared *runtime.PreparedProgram
	if prog != nil {
		if prog.Compiled != nil && prog.Compiled.Bytecode != nil && prog.Compiled.Bytecode.Executable != nil {
			prepared = prog.Compiled.Bytecode.Executable
		} else if prog.Program != nil {
			compiled, _, err := e.newCompiler().CompileProgram(path, "", prog.Program, false)
			if err == nil && compiled != nil && compiled.Bytecode != nil {
				prepared = compiled.Bytecode.Executable
			}
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if prog == nil {
		delete(e.modules, path)
		delete(e.moduleSources, path)
		return
	}
	delete(e.modules, path)
	if prog.Program != nil {
		e.moduleSources[path] = prog.Program
	} else {
		delete(e.moduleSources, path)
	}
	if prepared != nil {
		e.modules[path] = prepared
		return
	}
}
