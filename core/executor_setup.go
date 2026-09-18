package engine

import (
	"errors"
	"fmt"
	"sort"
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
		sourceLibraries:     make(map[string]surface.LibraryModule),
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
	res.mustAddFuncSchemaLocked("Byte", runtime.MustRuntimeFuncSig(runtime.SpecByte, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked("Rune", runtime.MustRuntimeFuncSig(runtime.SpecRune, false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked(runtime.ArrayType(runtime.SpecByte).String(), runtime.MustRuntimeFuncSig(runtime.ArrayType(runtime.SpecByte), false, runtime.SpecAny))
	res.mustAddFuncSchemaLocked(runtime.ArrayType(runtime.SpecRune).String(), runtime.MustRuntimeFuncSig(runtime.ArrayType(runtime.SpecRune), false, runtime.SpecAny))
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
		if err := e.validateSurfaceModuleNamespaceLocked(libraries, nextSchema, nil); err != nil {
			return err
		}
		resolvedLibraryHashes := e.libraryHashes
		if len(libraries) > 0 {
			existingLibraryASTs, err := e.parseRegisteredLibraryASTsLocked()
			if err != nil {
				return err
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
				for key, pkg := range bundle.Schema.Packages {
					if pkg == nil {
						continue
					}
					pkgPath := pkg.Path
					if pkgPath == "" {
						pkgPath = key
					}
					prefix := pkgPath + "."
					if strings.HasPrefix(name, prefix) {
						if _, ok := pkg.Members[strings.TrimPrefix(name, prefix)]; ok {
							return true
						}
					}
				}
				return false
			}
			nextTemplates = e.cloneTemplateRegistryLocked()
			for _, tpl := range bundle.Templates {
				if err := nextTemplates.Register(tpl); err != nil {
					return err
				}
			}
			if err := e.checkTemplateGlobalsLocked(nextTemplates, incomingSymbolExists); err != nil {
				return err
			}
		}
		nextBound := runtime.NewBoundFFISurface(nextSchema)
		if e.boundSurface != nil {
			if err := nextBound.Merge(e.boundSurface); err != nil {
				return err
			}
		}
		if bound != nil {
			if err := e.validateSurfaceModuleNamespaceLocked(libraries, nextSchema, bound); err != nil {
				return err
			}
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
		e.mu.RUnlock()
		if hasLibrary {
			return parseSurfaceLibraryModule(library)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}
}

func (e *MiniExecutor) applyExecutorConfig(executor *runtime.Executor) error {
	if executor == nil {
		return nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	return executor.ApplyBoundFFISurface(e.boundSurface)
}

func (e *MiniExecutor) newCompiler() *compiler.Compiler {
	return e.newCompilerWithModuleSources(nil, nil)
}

func (e *MiniExecutor) newCompilerWithModuleSources(stagedSources, importedPrograms map[string]*ast.ProgramStmt) *compiler.Compiler {
	return compiler.New(e.newCompilerConfigWithModuleSources(stagedSources, importedPrograms))
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
	if compiled == nil || compiled.Program == nil || compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		return nil
	}
	stagedPrepared := make(map[string]*runtime.PreparedProgram)
	stagedSources := make(map[string]*ast.ProgramStmt)
	if err := e.compileImportedModules(compiled.Program, compiled.ImportedPrograms, map[string]bool{}, stagedPrepared, stagedSources); err != nil {
		return newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
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
		library, hasLibrary := e.sourceLibraries[path]
		e.mu.RUnlock()
		prog := stagedSources[path]
		if hasLibrary {
			var err error
			prog, err = parseSurfaceLibraryModule(library)
			if err != nil {
				return err
			}
		} else if prog == nil && imported != nil {
			prog = imported[path]
		}
		if prog == nil {
			continue
		}
		prog.ModulePath = path
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

func (e *MiniExecutor) preparedModuleHashes(stagedPrepared map[string]*runtime.PreparedProgram) map[string]string {
	if len(stagedPrepared) == 0 {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	hashes := make(map[string]string, len(stagedPrepared))
	for path := range stagedPrepared {
		if hash := e.libraryHashes[path]; hash != "" {
			hashes[path] = hash
		}
	}
	return hashes
}

func attachPreparedModules(root *runtime.PreparedProgram, modules map[string]*runtime.PreparedProgram, hashes map[string]string) {
	if root == nil {
		return
	}
	if len(modules) > 0 {
		root.Modules = make(map[string]*runtime.PreparedProgram, len(modules))
		for path, prepared := range modules {
			if prepared != nil {
				root.Modules[path] = prepared
			}
		}
	}
	if len(hashes) > 0 {
		root.ModuleHashes = make(map[string]string, len(hashes))
		for path, hash := range hashes {
			if hash != "" && root.Modules[path] != nil {
				root.ModuleHashes[path] = hash
			}
		}
	}
}

func (e *MiniExecutor) preparedProgramForArtifact(artifact *ExecutableArtifact) (*runtime.PreparedProgram, error) {
	if artifact == nil || artifact.Bytecode == nil || artifact.Bytecode.Executable == nil {
		return nil, errors.New("executable artifact missing executable bytecode")
	}
	root := artifact.Bytecode.Executable
	if len(root.Modules) > 0 || len(root.ModuleHashes) > 0 {
		return nil, errors.New("bytecode executable must not embed source modules")
	}
	if err := e.validateSourceImportRequirements(root); err != nil {
		return nil, err
	}
	modules, hashes, err := e.prepareSourceRequirementModules(root)
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 {
		return root, nil
	}
	prepared := *root
	attachPreparedModules(&prepared, modules, hashes)
	return &prepared, nil
}

func (e *MiniExecutor) prepareSourceRequirementModules(root *runtime.PreparedProgram) (map[string]*runtime.PreparedProgram, map[string]string, error) {
	paths := sourceRequirementPaths(root)
	if len(paths) == 0 {
		return nil, nil, nil
	}
	program := &ast.ProgramStmt{Imports: make([]ast.ImportSpec, 0, len(paths))}
	for _, path := range paths {
		program.Imports = append(program.Imports, ast.ImportSpec{Path: path})
	}
	stagedPrepared := make(map[string]*runtime.PreparedProgram)
	stagedSources := make(map[string]*ast.ProgramStmt)
	if err := e.compileImportedModules(program, nil, map[string]bool{}, stagedPrepared, stagedSources); err != nil {
		return nil, nil, err
	}
	for _, path := range paths {
		if stagedPrepared[path] == nil {
			return nil, nil, fmt.Errorf("%w: %s", runtime.ErrModuleNotFound, path)
		}
	}
	return stagedPrepared, e.preparedModuleHashes(stagedPrepared), nil
}

func (e *MiniExecutor) validateSourceImportRequirements(root *runtime.PreparedProgram) error {
	if root == nil {
		return nil
	}
	required := sourceRequirementPathSet(root)
	imports := collectPreparedImportPaths(root)
	if len(imports) == 0 {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for path := range imports {
		if _, ok := required[path]; ok {
			continue
		}
		if _, ok := e.sourceLibraries[path]; ok {
			return fmt.Errorf("source import %s missing module requirement", path)
		}
	}
	return nil
}

func sourceRequirementPathSet(root *runtime.PreparedProgram) map[string]struct{} {
	paths := make(map[string]struct{})
	if root == nil {
		return paths
	}
	for _, req := range root.ModuleRequirements {
		path := strings.TrimSpace(req.Path)
		if req.Kind == runtime.ModuleKindSource && path != "" {
			paths[path] = struct{}{}
		}
	}
	return paths
}

func collectPreparedImportPaths(root *runtime.PreparedProgram) map[string]struct{} {
	paths := make(map[string]struct{})
	if root == nil {
		return paths
	}
	for _, path := range root.ImportAliases {
		if path = strings.TrimSpace(path); path != "" {
			paths[path] = struct{}{}
		}
	}
	collectImportPathsFromTasks(root.MainTasks, paths)
	for _, group := range root.GlobalInitGroups {
		if group != nil {
			collectImportPathsFromTasks(group.InitPlan, paths)
		}
	}
	for _, global := range root.Globals {
		if global != nil {
			collectImportPathsFromTasks(global.InitPlan, paths)
		}
	}
	for _, fn := range root.Functions {
		if fn != nil {
			collectImportPathsFromTasks(fn.BodyTasks, paths)
		}
	}
	return paths
}

func collectImportPathsFromTasks(tasks []runtime.Task, paths map[string]struct{}) {
	for _, task := range tasks {
		switch data := task.Data.(type) {
		case *runtime.ImportInitData:
			if data == nil {
				continue
			}
			path := strings.TrimSpace(data.Path)
			if path != "" {
				paths[path] = struct{}{}
			}
		case *runtime.BranchData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Then, paths)
			collectImportPathsFromTasks(data.Else, paths)
		case *runtime.DeferData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Tasks, paths)
		case *runtime.ForData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Cond, paths)
			collectImportPathsFromTasks(data.Body, paths)
			collectImportPathsFromTasks(data.Update, paths)
		case *runtime.JumpData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Right, paths)
		case *runtime.SwitchData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Init, paths)
			collectImportPathsFromTasks(data.Tag, paths)
			collectImportPathsFromTasks(data.AssignLHS, paths)
			for _, switchCase := range data.Cases {
				for _, expr := range switchCase.Exprs {
					collectImportPathsFromTasks(expr, paths)
				}
				collectImportPathsFromTasks(switchCase.Body, paths)
			}
			collectImportPathsFromTasks(data.DefaultBody, paths)
		case *runtime.FinallyData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Body, paths)
		case *runtime.CatchData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Body, paths)
		case *runtime.RangeData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.Body, paths)
		case *runtime.SelectData:
			if data == nil {
				continue
			}
			for _, selectCase := range data.Cases {
				collectImportPathsFromTasks(selectCase.Body, paths)
			}
		case *runtime.ClosureData:
			if data == nil {
				continue
			}
			collectImportPathsFromTasks(data.BodyTasks, paths)
		}
	}
}

func sourceRequirementPaths(root *runtime.PreparedProgram) []string {
	if root == nil {
		return nil
	}
	seen := sourceRequirementPathSet(root)
	if len(seen) == 0 {
		return nil
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
