package engine

import (
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (e *MiniExecutor) HandleRegistry() *ffigo.HandleRegistry {
	return e.registry
}

func (e *MiniExecutor) Executor() *runtime.Executor {
	executor, err := e.RuntimeExecutor()
	if err != nil {
		panic(err)
	}
	return executor
}

func (e *MiniExecutor) RuntimeExecutor() (*runtime.Executor, error) {
	executor, err := newEmptyRuntimeExecutor()
	if err != nil {
		return nil, err
	}
	if err := e.applyExecutorConfig(executor); err != nil {
		return nil, err
	}
	return executor, nil
}

func (e *MiniExecutor) RegisterFFISchema(name string, bridge ffigo.FFIBridge, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) {
	if err := e.TryRegisterFFISchema(name, bridge, methodID, sig, doc); err != nil {
		panic(err)
	}
}

func (e *MiniExecutor) TryRegisterFFISchema(name string, bridge ffigo.FFIBridge, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.registerFFISchemaLocked(name, bridge, methodID, sig, doc)
}

// RegisterFunctionTemplate registers a compiler-only call template.
//
// Templates participate in semantic checking and are expanded before bytecode
// generation. They do not register runtime FFI routes.
func (e *MiniExecutor) RegisterFunctionTemplate(tpl calltemplate.FunctionTemplate) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.templates == nil {
		e.templates = calltemplate.NewRegistry()
	}
	next := e.templates.Clone()
	if next == nil {
		next = calltemplate.NewRegistry()
	}
	if err := next.Register(tpl); err != nil {
		return err
	}
	for name, registered := range next.Globals() {
		if e.globalSymbolExistsLocked(name) {
			return fmt.Errorf("global call template %s conflicts with existing symbol %s", registered.ID, name)
		}
	}
	e.templates = next
	return nil
}

// RegisterConstant 注册一个全局常量到执行器
func (e *MiniExecutor) RegisterConstant(name, val string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mustNotConflictGlobalTemplateLocked(name, "constant")
	e.constants[name] = val
}

func (e *MiniExecutor) GetExportedConstants() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	res := make(map[string]string)
	for k, v := range e.constants {
		res[k] = v
	}
	return res
}

// DeclareFuncSchema 仅用于在验证阶段声明一个合法的外部函数。
func (e *MiniExecutor) DeclareFuncSchema(name string, sig *runtime.RuntimeFuncSig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sig == nil {
		delete(e.funcSchemas, ast.Ident(name))
		return
	}
	e.mustNotConflictGlobalTemplateLocked(name, "function")
	e.funcSchemas[ast.Ident(name)] = sig
}

func (e *MiniExecutor) RegisterStructSchema(name string, spec *runtime.RuntimeStructSpec) {
	if err := e.TryRegisterStructSchema(name, spec); err != nil {
		panic(err)
	}
}

func (e *MiniExecutor) TryRegisterStructSchema(name string, spec *runtime.RuntimeStructSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.registerStructSchemaLocked(name, spec)
}

func (e *MiniExecutor) RegisterInterfaceSchema(name string, spec *runtime.RuntimeInterfaceSpec) {
	if err := e.TryRegisterInterfaceSchema(name, spec); err != nil {
		panic(err)
	}
}

func (e *MiniExecutor) TryRegisterInterfaceSchema(name string, spec *runtime.RuntimeInterfaceSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.registerInterfaceSchemaLocked(name, spec)
}

// DeclareStructSchema 仅用于在验证阶段声明一个合法的外部结构体 schema。
func (e *MiniExecutor) DeclareStructSchema(name string, spec *runtime.RuntimeStructSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.registerStructSchemaLocked(name, spec); err != nil {
		panic(err)
	}
}

func (e *MiniExecutor) DeclareInterfaceSchema(name string, spec *runtime.RuntimeInterfaceSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.registerInterfaceSchemaLocked(name, spec); err != nil {
		panic(err)
	}
}

func (e *MiniExecutor) ExportedSchema() *ExportedSchemaSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	res := &ExportedSchemaSnapshot{
		Funcs:      make(map[ast.Ident]*runtime.RuntimeFuncSig, len(e.funcSchemas)),
		Structs:    make(map[ast.Ident]*runtime.RuntimeStructSpec, len(e.structsMeta)),
		Interfaces: make(map[ast.Ident]*runtime.RuntimeInterfaceSpec, len(e.interfacesMeta)),
	}
	for k, v := range e.funcSchemas {
		res.Funcs[k] = runtime.CloneRuntimeFuncSig(v)
	}
	for k, v := range e.structsMeta {
		res.Structs[k] = runtime.CloneRuntimeStructSpec(v)
	}
	for k, v := range e.interfacesMeta {
		res.Interfaces[k] = runtime.CloneRuntimeInterfaceSpec(v)
	}
	return res
}

func (e *MiniExecutor) templateRegistrySnapshot() *calltemplate.Registry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.templates == nil {
		return nil
	}
	return e.templates.Clone()
}

func (e *MiniExecutor) globalSymbolExistsLocked(name string) bool {
	if _, ok := e.funcSchemas[ast.Ident(name)]; ok {
		return true
	}
	if _, ok := e.routes[name]; ok {
		return true
	}
	if _, ok := e.constants[name]; ok {
		return true
	}
	if _, ok := e.structsMeta[ast.Ident(name)]; ok {
		return true
	}
	if _, ok := e.interfacesMeta[ast.Ident(name)]; ok {
		return true
	}
	return false
}

func (e *MiniExecutor) registerFFISchemaLocked(name string, bridge ffigo.FFIBridge, methodID uint32, funcSig *runtime.RuntimeFuncSig, doc string) error {
	if funcSig != nil {
		if err := e.checkGlobalTemplateConflictLocked(name, "function"); err != nil {
			return err
		}
	}
	next := runtime.FFIRoute{
		Name:     name,
		Bridge:   bridge,
		MethodID: methodID,
		Doc:      doc,
		FuncSig:  funcSig,
	}
	if existing, ok := e.routes[name]; ok {
		if err := runtime.CheckRouteCompatible(name, existing, next); err != nil {
			return err
		}
	}
	e.routes[name] = next
	if funcSig != nil {
		if existing, ok := e.funcSchemas[ast.Ident(name)]; ok && !runtime.SameRuntimeFuncSchema(existing, funcSig) {
			return &runtime.SchemaConflictError{
				Kind:     "schema",
				Name:     name,
				Existing: string(existing.Spec),
				New:      string(funcSig.Spec),
			}
		}
		e.funcSchemas[ast.Ident(name)] = funcSig
	}
	return nil
}

func (e *MiniExecutor) registerStructSchemaLocked(name string, spec *runtime.RuntimeStructSpec) error {
	if spec != nil {
		if err := e.checkGlobalTemplateConflictLocked(name, "struct"); err != nil {
			return err
		}
		if existing, ok := e.structsMeta[ast.Ident(name)]; ok {
			merged, err := runtime.MergeStructSchema(name, existing, spec)
			if err != nil {
				return err
			}
			spec = merged
		}
		e.structsMeta[ast.Ident(name)] = spec
		return nil
	}
	delete(e.structsMeta, ast.Ident(name))
	return nil
}

func (e *MiniExecutor) registerInterfaceSchemaLocked(name string, spec *runtime.RuntimeInterfaceSpec) error {
	if spec != nil {
		if err := e.checkGlobalTemplateConflictLocked(name, "interface"); err != nil {
			return err
		}
		if existing, ok := e.interfacesMeta[ast.Ident(name)]; ok {
			if err := runtime.CheckInterfaceSchemaCompatible(name, existing, spec); err != nil {
				return err
			}
		}
		e.interfacesMeta[ast.Ident(name)] = spec
		return nil
	}
	delete(e.interfacesMeta, ast.Ident(name))
	return nil
}

func (e *MiniExecutor) mustAddFuncSchemaLocked(name string, sig *runtime.RuntimeFuncSig) {
	if sig == nil {
		panic("invalid builtin function schema: " + name)
	}
	e.mustNotConflictGlobalTemplateLocked(name, "function")
	e.funcSchemas[ast.Ident(name)] = sig
}

func (e *MiniExecutor) mustNotConflictGlobalTemplateLocked(name, kind string) {
	if err := e.checkGlobalTemplateConflictLocked(name, kind); err != nil {
		panic(err)
	}
}

func (e *MiniExecutor) checkGlobalTemplateConflictLocked(name, kind string) error {
	if e.templates == nil {
		return nil
	}
	if tpl, ok := e.templates.Global(name); ok {
		return fmt.Errorf("%s %s conflicts with global call template %s", kind, name, tpl.ID)
	}
	return nil
}

func (e *MiniExecutor) formatRouteSchema(route runtime.FFIRoute) string {
	spec := ast.GoMiniType("")
	if route.FuncSig != nil {
		spec = ast.GoMiniType(route.FuncSig.Spec)
	}
	return e.formatSchemaWithDoc(spec, route.Doc, route.FuncSig)
}

func (e *MiniExecutor) formatSchemaWithDoc(spec ast.GoMiniType, doc string, parsed *runtime.RuntimeFuncSig) string {
	sig := string(spec)
	if parsed != nil {
		sig = string(parsed.Spec)
	}
	if doc != "" {
		sig += " // " + strings.ReplaceAll(doc, "\n", " ")
	}
	return sig
}
