package engine

import (
	"fmt"
	"reflect"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/ffilib/iolib"
	"gopkg.d7z.net/go-mini/core/ffilib/oslib"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (e *MiniExecutor) HandleRegistry() *ffigo.HandleRegistry {
	return e.registry
}

func (e *MiniExecutor) Executor() *runtime.Executor {
	executor, _ := newEmptyRuntimeExecutor()
	e.applyExecutorConfig(executor)
	return executor
}

func (e *MiniExecutor) RegisterFFISchema(name string, bridge ffigo.FFIBridge, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerFFISchemaLocked(name, bridge, methodID, sig, doc)
}

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

func (e *MiniExecutor) InjectStandardLibraries() {
	// 1. Inject os
	oslib.RegisterOS(e, &oslib.OSHost{}, e.registry)

	// 2. Inject file-backed io handles and methods.
	iolib.RegisterFile(e, e.registry)
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
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerStructSchemaLocked(name, spec)
}

func (e *MiniExecutor) RegisterInterfaceSchema(name string, spec *runtime.RuntimeInterfaceSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerInterfaceSchemaLocked(name, spec)
}

// DeclareStructSchema 仅用于在验证阶段声明一个合法的外部结构体 schema。
func (e *MiniExecutor) DeclareStructSchema(name string, spec *runtime.RuntimeStructSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerStructSchemaLocked(name, spec)
}

func (e *MiniExecutor) DeclareInterfaceSchema(name string, spec *runtime.RuntimeInterfaceSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerInterfaceSchemaLocked(name, spec)
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

func (e *MiniExecutor) registerFFISchemaLocked(name string, bridge ffigo.FFIBridge, methodID uint32, funcSig *runtime.RuntimeFuncSig, doc string) {
	if funcSig != nil {
		e.mustNotConflictGlobalTemplateLocked(name, "function")
	}
	next := runtime.FFIRoute{
		Name:     name,
		Bridge:   bridge,
		MethodID: methodID,
		Doc:      doc,
		FuncSig:  funcSig,
	}
	if existing, ok := e.routes[name]; ok {
		ensureCompatibleRoute(name, existing, next)
	}
	e.routes[name] = next
	if funcSig != nil {
		if existing, ok := e.funcSchemas[ast.Ident(name)]; ok && !sameRuntimeFuncSig(existing, funcSig) {
			panic(fmt.Sprintf("ffi schema conflict for %s: existing=%s new=%s", name, existing.Spec, funcSig.Spec))
		}
		e.funcSchemas[ast.Ident(name)] = funcSig
	}
}

func (e *MiniExecutor) registerStructSchemaLocked(name string, spec *runtime.RuntimeStructSpec) {
	if spec != nil {
		e.mustNotConflictGlobalTemplateLocked(name, "struct")
		if existing, ok := e.structsMeta[ast.Ident(name)]; ok {
			merged, ok := mergeRuntimeStructSpec(existing, spec)
			if !ok {
				panic(fmt.Sprintf("ffi struct schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
			}
			spec = merged
		}
		e.structsMeta[ast.Ident(name)] = spec
		return
	}
	delete(e.structsMeta, ast.Ident(name))
}

func (e *MiniExecutor) registerInterfaceSchemaLocked(name string, spec *runtime.RuntimeInterfaceSpec) {
	if spec != nil {
		e.mustNotConflictGlobalTemplateLocked(name, "interface")
		if existing, ok := e.interfacesMeta[ast.Ident(name)]; ok && existing.Spec != spec.Spec {
			panic(fmt.Sprintf("ffi interface schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
		}
		e.interfacesMeta[ast.Ident(name)] = spec
		return
	}
	delete(e.interfacesMeta, ast.Ident(name))
}

func (e *MiniExecutor) mustAddFuncSchemaLocked(name string, sig *runtime.RuntimeFuncSig) {
	if sig == nil {
		panic("invalid builtin function schema: " + name)
	}
	e.mustNotConflictGlobalTemplateLocked(name, "function")
	e.funcSchemas[ast.Ident(name)] = sig
}

func (e *MiniExecutor) mustNotConflictGlobalTemplateLocked(name, kind string) {
	if e.templates == nil {
		return
	}
	if tpl, ok := e.templates.GlobalTemplate(name); ok {
		panic(fmt.Sprintf("%s %s conflicts with global call template %s", kind, name, tpl.ID))
	}
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

func routeConflictError(name string, existing, next runtime.FFIRoute) string {
	return fmt.Sprintf(
		"ffi route conflict for %s: existing(method=%d sig=%s bridge=%s) new(method=%d sig=%s bridge=%s)",
		name,
		existing.MethodID,
		runtimeRouteSignature(existing),
		bridgeIdentity(existing.Bridge),
		next.MethodID,
		runtimeRouteSignature(next),
		bridgeIdentity(next.Bridge),
	)
}

func ensureCompatibleRoute(name string, existing, next runtime.FFIRoute) {
	if existing.Name != next.Name ||
		existing.MethodID != next.MethodID ||
		existing.Doc != next.Doc ||
		!sameRuntimeFuncSig(existing.FuncSig, next.FuncSig) ||
		!sameBridge(existing.Bridge, next.Bridge) {
		panic(routeConflictError(name, existing, next))
	}
}

func runtimeRouteSignature(route runtime.FFIRoute) string {
	if route.FuncSig != nil {
		return string(route.FuncSig.Spec)
	}
	return ""
}

func sameRuntimeFuncSig(a, b *runtime.RuntimeFuncSig) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.Spec == b.Spec
	}
}

func sameRuntimeStructSpec(a, b *runtime.RuntimeStructSpec) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.TypeID == b.TypeID && a.Spec == b.Spec && a.Name == b.Name && a.Ownership == b.Ownership
	}
}

func mergeRuntimeStructSpec(existing, next *runtime.RuntimeStructSpec) (*runtime.RuntimeStructSpec, bool) {
	switch {
	case existing == nil || next == nil:
		return next, existing == next
	case sameRuntimeStructSpec(existing, next):
		return existing, true
	}
	return nil, false
}

func sameBridge(a, b ffigo.FFIBridge) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	return ta == tb
}

func bridgeIdentity(bridge ffigo.FFIBridge) string {
	if bridge == nil {
		return "<nil>"
	}
	v := reflect.ValueOf(bridge)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Sprintf("%T@0x%x", bridge, v.Pointer())
	default:
		return fmt.Sprintf("%T:%v", bridge, bridge)
	}
}
