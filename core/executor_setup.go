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

func NewMiniExecutor() (*MiniExecutor, error) {
	res := &MiniExecutor{
		routes:              make(map[string]runtime.FFIRoute),
		constants:           make(map[string]runtime.FFIConstValue),
		constTypes:          make(map[string]runtime.RuntimeType),
		registry:            ffigo.NewHandleRegistry(),
		moduleSources:       make(map[string]*ast.ProgramStmt),
		sourceLibraries:     make(map[string]surface.LibraryModule),
		modules:             make(map[string]*runtime.PreparedProgram),
		librarySourceHashes: make(map[string]string),
		libraryHashes:       make(map[string]string),
		funcSchemas:         make(map[ast.Ident]*runtime.RuntimeFuncSig),
		valueSchemas:        make(map[ast.Ident]*runtime.ValueSpec),
		packageValues:       make(map[string]*runtime.BoundPackageValue),
		surfaceSchema:       runtime.NewFFISurfaceSchema(),
		boundSurface:        runtime.NewBoundFFISurface(runtime.NewFFISurfaceSchema()),
		structsMeta:         make(map[ast.Ident]*runtime.RuntimeStructSpec),
		interfacesMeta:      make(map[ast.Ident]*runtime.RuntimeInterfaceSpec),
		templates:           calltemplate.NewRegistry(),
		MaxTypeDepth:        256,
	}

	// 默认注册 panic 签名以便通过验证
	res.mustAddFuncSchemaLocked("panic", runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("recover", runtime.MustRuntimeFuncSig(runtime.SpecError, false))
	res.mustAddFuncSchemaLocked("String", runtime.MustRuntimeFuncSig(runtime.SpecString, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("TypeBytes", runtime.MustRuntimeFuncSig(runtime.SpecBytes, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("len", runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("cap", runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("make", runtime.MustRuntimeFuncSig(runtime.SpecAny, true, runtime.SpecString, runtime.SpecInt64))
	res.mustAddFuncSchemaLocked("new", runtime.MustRuntimeFuncSig(runtime.SpecAny, false, runtime.SpecString))
	res.mustAddFuncSchemaLocked("append", runtime.MustRuntimeFuncSig(runtime.SpecAny, true, runtime.SpecAny, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("delete", runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("close", runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("Int64", runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("Float64", runtime.MustRuntimeFuncSig(runtime.SpecFloat64, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("Bool", runtime.MustRuntimeFuncSig(runtime.SpecBool, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("require", runtime.MustRuntimeFuncSig(runtime.SpecModule, false, runtime.SpecString))

	if err := res.UseSurface(coreffilib.Surface()); err != nil {
		return nil, err
	}

	return res, nil
}

func MustNewMiniExecutor() *MiniExecutor {
	res, err := NewMiniExecutor()
	if err != nil {
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
	libraries, libraryASTs, librarySourceHashes, err := prepareSurfaceLibraryModules(bundle.Libraries)
	if err != nil {
		return err
	}
	if bundle.Schema != nil || bundle.Bind != nil || len(bundle.Templates) > 0 || len(libraries) > 0 {
		e.mu.Lock()
		defer e.mu.Unlock()

		nextSchema := runtime.CloneFFISurfaceSchema(e.surfaceSchema)
		if nextSchema == nil {
			nextSchema = runtime.NewFFISurfaceSchema()
		}
		if err := nextSchema.Merge(bundle.Schema); err != nil {
			return err
		}
		if err := e.validateSurfaceLibrariesLocked(libraries, librarySourceHashes); err != nil {
			return err
		}
		resolvedLibraryHashes := e.libraryHashes
		if len(libraries) > 0 {
			existingLibraryASTs := make(map[string]*ast.ProgramStmt, len(e.librarySourceHashes))
			for path := range e.librarySourceHashes {
				library, ok := e.sourceLibraries[path]
				if !ok {
					continue
				}
				program, err := parseSurfaceLibraryModule(library)
				if err != nil {
					return err
				}
				existingLibraryASTs[path] = program
			}
			allASTs := make(map[string]*ast.ProgramStmt, len(e.librarySourceHashes)+len(libraryASTs))
			allSourceHashes := make(map[string]string, len(e.librarySourceHashes)+len(librarySourceHashes))
			for path, hash := range e.librarySourceHashes {
				allSourceHashes[path] = hash
				allASTs[path] = existingLibraryASTs[path]
			}
			for path, hash := range librarySourceHashes {
				allSourceHashes[path] = hash
				allASTs[path] = libraryASTs[path]
			}
			resolvedLibraryHashes, err = resolveSurfaceLibraryHashes(allASTs, allSourceHashes)
			if err != nil {
				return err
			}
		}

		var bound *runtime.BoundFFISurface
		var registryTx *ffigo.HandleRegistryTx
		if bundle.Bind != nil {
			registryTx = e.registry.BeginTransaction()
			defer registryTx.Rollback()

			var err error
			bound, err = bundle.Bind(runtime.FFIBindContext{Registry: registryTx.Registry, PinnedRegistry: registryTx.Registry})
			if err != nil {
				return err
			}
		}

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
		if bound != nil {
			registryTx.Commit()
		}
		if len(libraries) > 0 {
			e.applySurfaceLibrariesLocked(libraries, librarySourceHashes, resolvedLibraryHashes)
		}
		e.surfaceSchema = runtime.CloneFFISurfaceSchema(nextBound.Schema)
		e.boundSurface = nextBound
		if nextTemplates != nil {
			e.templates = nextTemplates
		}
	}
	return nil
}

func (e *MiniExecutor) moduleASTLoaderWithStaged(stagedSources, importedPrograms map[string]*ast.ProgramStmt) func(path string) (*ast.ProgramStmt, error) {
	return func(path string) (*ast.ProgramStmt, error) {
		if prog := stagedSources[path]; prog != nil {
			return prog, nil
		}
		if prog := importedPrograms[path]; prog != nil {
			return prog, nil
		}
		e.mu.RLock()
		library, hasLibrary := e.sourceLibraries[path]
		astNode, ok := e.moduleSources[path]
		e.mu.RUnlock()
		if hasLibrary {
			return parseSurfaceLibraryModule(library)
		}
		if ok {
			return astNode, nil
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
		return e.prepareModuleFromSource(path)
	}
}

func (e *MiniExecutor) applyExecutorConfig(executor *runtime.Executor) error {
	if executor == nil {
		return nil
	}
	executor.ModulePlanLoader = e.modulePlanLoader()

	e.mu.RLock()
	defer e.mu.RUnlock()
	if err := executor.ApplyBoundFFISurface(e.boundSurface); err != nil {
		return err
	}
	for path, hash := range e.libraryHashes {
		executor.RegisterModuleHash(path, hash)
	}
	return nil
}

func (e *MiniExecutor) newCompiler() *compiler.Compiler {
	return e.newCompilerWithModuleSources(nil, nil)
}

func (e *MiniExecutor) newCompilerWithModuleSources(stagedSources, importedPrograms map[string]*ast.ProgramStmt) *compiler.Compiler {
	schema := e.ExportedSchema()
	e.mu.RLock()
	surfaceSchema := runtime.CloneFFISurfaceSchema(e.surfaceSchema)
	moduleHashes := cloneSurfaceLibraryHashes(e.libraryHashes)
	e.mu.RUnlock()
	return compiler.New(compiler.Config{
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

func (e *MiniExecutor) prepareCompiledArtifact(compiled *compiler.Artifact, semanticCtx *ast.SemanticContext) error {
	if err := e.prepareArtifactModules(compiled); err != nil {
		return newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	return nil
}

func (e *MiniExecutor) prepareArtifactModules(compiled *compiler.Artifact) error {
	if compiled == nil || compiled.Program == nil {
		return nil
	}
	stagedPrepared := make(map[string]*runtime.PreparedProgram)
	stagedSources := make(map[string]*ast.ProgramStmt)
	if err := e.compileImportedModules(compiled.Program, compiled.ImportedPrograms, map[string]bool{}, stagedPrepared, stagedSources); err != nil {
		return err
	}
	e.commitPreparedModuleStage(stagedPrepared, stagedSources)
	return nil
}

func (e *MiniExecutor) compileImportedModules(program *ast.ProgramStmt, imported map[string]*ast.ProgramStmt, visiting map[string]bool, stagedPrepared map[string]*runtime.PreparedProgram, stagedSources map[string]*ast.ProgramStmt) error {
	if program == nil {
		return nil
	}
	for _, imp := range program.Imports {
		path := strings.TrimSpace(imp.Path)
		if path == "" {
			continue
		}
		if stagedPrepared[path] != nil {
			continue
		}
		if stagedSources[path] != nil && visiting[path] {
			return fmt.Errorf("circular module dependency while preparing %s", path)
		}

		e.mu.RLock()
		prepared := e.modules[path]
		library, hasLibrary := e.sourceLibraries[path]
		registeredSource := e.moduleSources[path]
		e.mu.RUnlock()
		if prepared != nil {
			continue
		}
		prog := stagedSources[path]
		if hasLibrary {
			var err error
			prog, err = parseSurfaceLibraryModule(library)
			if err != nil {
				return err
			}
		} else if prog == nil && imported != nil {
			prog = imported[path]
		} else if prog == nil {
			prog = registeredSource
		}
		if prog == nil {
			continue
		}
		if visiting[path] {
			return fmt.Errorf("circular module dependency while preparing %s", path)
		}

		visiting[path] = true
		if !hasLibrary {
			stagedSources[path] = prog
		}
		compiled, _, err := e.newCompilerWithModuleSources(stagedSources, imported).CompileProgram(path, "", prog, false)
		if err != nil {
			delete(visiting, path)
			return fmt.Errorf("compile module %s: %w", path, err)
		}
		if compiled == nil || compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
			delete(visiting, path)
			return fmt.Errorf("module %s did not produce executable bytecode", path)
		}

		stagedPrepared[path] = compiled.Bytecode.Executable

		if err := e.compileImportedModules(compiled.Program, compiled.ImportedPrograms, visiting, stagedPrepared, stagedSources); err != nil {
			delete(visiting, path)
			return err
		}
		delete(visiting, path)
	}
	return nil
}

func (e *MiniExecutor) commitPreparedModuleStage(stagedPrepared map[string]*runtime.PreparedProgram, stagedSources map[string]*ast.ProgramStmt) {
	if len(stagedPrepared) == 0 && len(stagedSources) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for path, prepared := range stagedPrepared {
		if prepared != nil {
			e.modules[path] = prepared
		}
	}
	for path, prog := range stagedSources {
		if prog == nil {
			continue
		}
		if _, hasLibrary := e.sourceLibraries[path]; hasLibrary {
			continue
		}
		e.moduleSources[path] = prog
	}
}

func newEmptyRuntimeExecutor() (*runtime.Executor, error) {
	return runtime.NewExecutorFromPrepared(&runtime.PreparedProgram{
		Globals:   map[string]*runtime.PreparedGlobal{},
		Functions: map[string]*runtime.PreparedFunction{},
		MainTasks: []runtime.Task{},
	})
}

// RegisterModule 注册一个预编译的模块，使得脚本可以通过 import 直接引用
func (e *MiniExecutor) RegisterModule(path string, prog *ExecutableProgram) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if prog == nil {
		delete(e.modules, path)
		delete(e.moduleSources, path)
		delete(e.sourceLibraries, path)
		delete(e.librarySourceHashes, path)
		delete(e.libraryHashes, path)
		e.recomputeSurfaceLibraryHashesLocked()
		return
	}

	var prepared *runtime.PreparedProgram
	if compiled := prog.Compilation(); compiled != nil && compiled.Bytecode != nil {
		prepared = compiled.Bytecode.Executable
	}
	delete(e.moduleSources, path)
	delete(e.sourceLibraries, path)
	if prepared != nil {
		e.modules[path] = prepared
		delete(e.librarySourceHashes, path)
		delete(e.libraryHashes, path)
		e.recomputeSurfaceLibraryHashesLocked()
		return
	}
	delete(e.modules, path)
	delete(e.librarySourceHashes, path)
	delete(e.libraryHashes, path)
	e.recomputeSurfaceLibraryHashesLocked()
}
