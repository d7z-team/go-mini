package engine

import (
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// RegisterFunctionTemplate registers a compiler-only call template.
//
// Templates participate in semantic checking and are expanded before bytecode
// generation. They do not register runtime FFI routes.
func (e *MiniExecutor) RegisterFunctionTemplate(tpl calltemplate.FunctionTemplate) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	next := e.cloneTemplateRegistryLocked()
	if err := next.Register(tpl); err != nil {
		return err
	}
	if err := e.checkTemplateGlobalsLocked(next, nil); err != nil {
		return err
	}
	e.templates = next
	return nil
}

func (e *MiniExecutor) GetExportedConstants() map[string]runtime.FFIConstValue {
	e.mu.RLock()
	defer e.mu.RUnlock()
	res := make(map[string]runtime.FFIConstValue)
	for k, v := range e.constants {
		res[k] = v
	}
	return res
}

func (e *MiniExecutor) ExportedSchema() *ExportedSchemaSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	res := &ExportedSchemaSnapshot{
		Funcs:                   make(map[ast.Ident]*runtime.RuntimeFuncSig, len(e.funcSchemas)),
		RegisteredFuncs:         make(map[ast.Ident]bool, len(e.routes)),
		RegisteredFuncMethodIDs: make(map[ast.Ident]uint32, len(e.routes)),
		Values:                  make(map[ast.Ident]*runtime.ValueSpec, len(e.valueSchemas)),
		Structs:                 make(map[ast.Ident]*runtime.RuntimeStructSpec, len(e.structsMeta)),
		Interfaces:              make(map[ast.Ident]*runtime.RuntimeInterfaceSpec, len(e.interfacesMeta)),
		Constants:               make(map[string]runtime.FFIConstValue, len(e.constants)),
	}
	for k, v := range e.funcSchemas {
		res.Funcs[k] = runtime.CloneRuntimeFuncSig(v)
	}
	for name, route := range e.routes {
		res.RegisteredFuncs[ast.Ident(name)] = true
		res.RegisteredFuncMethodIDs[ast.Ident(name)] = route.MethodID
	}
	for k, v := range e.valueSchemas {
		if v == nil {
			continue
		}
		res.Values[k] = &runtime.ValueSpec{
			Type:     v.Type,
			Doc:      v.Doc,
			ReadOnly: v.ReadOnly,
		}
	}
	for k, v := range e.structsMeta {
		res.Structs[k] = runtime.CloneRuntimeStructSpec(v)
	}
	for k, v := range e.interfacesMeta {
		res.Interfaces[k] = runtime.CloneRuntimeInterfaceSpec(v)
	}
	for k, v := range e.constants {
		res.Constants[k] = v
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

func (e *MiniExecutor) cloneTemplateRegistryLocked() *calltemplate.Registry {
	if e.templates == nil {
		return calltemplate.NewRegistry()
	}
	next := e.templates.Clone()
	if next == nil {
		return calltemplate.NewRegistry()
	}
	return next
}

func (e *MiniExecutor) checkTemplateGlobalsLocked(next *calltemplate.Registry, incomingSymbolExists func(string) bool) error {
	if next == nil {
		return nil
	}
	for name, registered := range next.Globals() {
		switch {
		case e.globalSymbolExistsLocked(name):
			return fmt.Errorf("global call template %s conflicts with existing symbol %s", registered.ID, name)
		case incomingSymbolExists != nil && incomingSymbolExists(name):
			return fmt.Errorf("global call template %s conflicts with existing symbol %s", registered.ID, name)
		}
	}
	return nil
}

func (e *MiniExecutor) globalSymbolExistsLocked(name string) bool {
	if _, ok := e.funcSchemas[ast.Ident(name)]; ok {
		return true
	}
	if _, ok := e.routes[name]; ok {
		return true
	}
	if _, ok := e.valueSchemas[ast.Ident(name)]; ok {
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

func (e *MiniExecutor) validateBoundSurfaceLocked(bound *runtime.BoundFFISurface) error {
	if bound == nil {
		return nil
	}
	for name, route := range bound.Routes {
		if err := runtime.CheckPublicFFIRouteSchema(name, route); err != nil {
			return err
		}
		if route.FuncSig != nil {
			if err := e.checkGlobalTemplateConflictLocked(name, "function"); err != nil {
				return err
			}
		}
		if existing, ok := e.routes[name]; ok {
			if err := runtime.CheckRouteCompatible(name, existing, route); err != nil {
				return err
			}
		}
		if route.FuncSig != nil {
			ident := ast.Ident(name)
			if existing, ok := e.funcSchemas[ident]; ok && !runtime.SameRuntimeFuncSchema(existing, route.FuncSig) {
				return &runtime.SchemaConflictError{
					Kind:     "schema",
					Name:     name,
					Existing: string(existing.Spec),
					New:      string(route.FuncSig.Spec),
				}
			}
		}
	}
	for name, spec := range bound.Structs {
		if spec == nil {
			continue
		}
		if err := runtime.CheckPublicFFIStructSchema(name, spec); err != nil {
			return err
		}
		if err := e.checkGlobalTemplateConflictLocked(name, "struct"); err != nil {
			return err
		}
		if existing, ok := e.structsMeta[ast.Ident(name)]; ok {
			if _, err := runtime.MergeStructSchema(name, existing, spec); err != nil {
				return err
			}
		}
	}
	for name, spec := range bound.Interfaces {
		if spec == nil {
			continue
		}
		if err := runtime.CheckPublicFFIInterfaceSchema(name, spec); err != nil {
			return err
		}
		if err := e.checkGlobalTemplateConflictLocked(name, "interface"); err != nil {
			return err
		}
		if existing, ok := e.interfacesMeta[ast.Ident(name)]; ok {
			if err := runtime.CheckInterfaceSchemaCompatible(name, existing, spec); err != nil {
				return err
			}
		}
	}
	for name, value := range bound.PackageValues {
		if value == nil || value.Spec == nil {
			continue
		}
		if err := runtime.CheckPublicFFIValueSpec(name, value.Spec); err != nil {
			return err
		}
		if err := e.checkGlobalTemplateConflictLocked(name, "package value"); err != nil {
			return err
		}
		if existing, ok := e.valueSchemas[ast.Ident(name)]; ok && existing.Type.Raw != value.Spec.Type.Raw {
			return &runtime.SchemaConflictError{
				Kind:     "package value",
				Name:     name,
				Existing: existing.Type.Raw.String(),
				New:      value.Spec.Type.Raw.String(),
			}
		}
	}
	for name, val := range bound.Consts {
		if err := val.Validate(); err != nil {
			return fmt.Errorf("constant %s invalid: %w", name, err)
		}
		if err := e.checkGlobalTemplateConflictLocked(name, "constant"); err != nil {
			return err
		}
		if existing, ok := e.constants[name]; ok && existing.Hash() != val.Hash() {
			return &runtime.SchemaConflictError{
				Kind:     "constant",
				Name:     name,
				Existing: existing.Hash(),
				New:      val.Hash(),
			}
		}
		typ, _ := runtime.ParseRuntimeType(val.Type)
		if existing, ok := e.constTypes[name]; ok && existing.Raw != typ.Raw {
			return &runtime.SchemaConflictError{
				Kind:     "constant",
				Name:     name,
				Existing: existing.Raw.String(),
				New:      typ.Raw.String(),
			}
		}
	}
	return nil
}

func (e *MiniExecutor) applyBoundSurfaceChangesLocked(bound *runtime.BoundFFISurface) {
	if bound == nil {
		return
	}
	for name, route := range bound.Routes {
		e.routes[name] = route
		if route.FuncSig != nil {
			e.funcSchemas[ast.Ident(name)] = runtime.CloneRuntimeFuncSig(route.FuncSig)
		}
	}
	for name, spec := range bound.Structs {
		if spec == nil {
			continue
		}
		if existing, ok := e.structsMeta[ast.Ident(name)]; ok {
			if merged, err := runtime.MergeStructSchema(name, existing, spec); err == nil {
				spec = merged
			}
		}
		e.structsMeta[ast.Ident(name)] = runtime.CloneRuntimeStructSpec(spec)
	}
	for name, spec := range bound.Interfaces {
		if spec == nil {
			continue
		}
		e.interfacesMeta[ast.Ident(name)] = runtime.CloneRuntimeInterfaceSpec(spec)
	}
	for name, value := range bound.PackageValues {
		if value == nil || value.Spec == nil {
			continue
		}
		e.valueSchemas[ast.Ident(name)] = &runtime.ValueSpec{Type: value.Spec.Type, Doc: value.Spec.Doc, ReadOnly: value.Spec.ReadOnly}
		e.packageValues[name] = value
	}
	for name, val := range bound.Consts {
		e.constants[name] = val
		if typ, err := runtime.ParseRuntimeType(val.Type); err == nil && !typ.IsEmpty() {
			e.constTypes[name] = typ
		}
	}
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
