package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

func (e *Executor) RegisterRoute(name string, route FFIRoute) {
	if err := e.TryRegisterRoute(name, route); err != nil {
		panic(err)
	}
}

func (e *Executor) TryRegisterRoute(name string, route FFIRoute) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if route.Name == "" {
		route.Name = name
	}
	if err := CheckPublicFFIRouteSchema(name, route); err != nil {
		return err
	}
	if existing, ok := e.routes[name]; ok {
		if err := CheckRouteCompatible(name, existing, route); err != nil {
			return err
		}
	}
	e.routes[name] = route
	if pkg, member := SplitExternalName(name); pkg != "" && member != "" && !strings.Contains(member, ".") {
		typ := RuntimeType{}
		if route.FuncSig != nil {
			if parsed, err := ParseRuntimeType(route.FuncSig.Spec); err == nil {
				typ = parsed
			}
		}
		e.registerBoundPackageMemberLocked(pkg, &BoundFFIMember{
			Name:      member,
			Kind:      FFIMemberFunc,
			Type:      typ,
			ReadOnly:  true,
			RouteName: name,
		})
	}
	return nil
}

func (e *Executor) RegisterStructSchema(name string, spec *RuntimeStructSpec) {
	if err := e.TryRegisterStructSchema(name, spec); err != nil {
		panic(err)
	}
}

func (e *Executor) TryRegisterStructSchema(name string, spec *RuntimeStructSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if spec == nil {
		e.metadata.registerStructSchema(name, nil)
		return nil
	}
	if err := CheckPublicFFIStructSchema(name, spec); err != nil {
		return err
	}
	if existing, ok := e.metadata.structsByName[name]; ok {
		merged, err := MergeStructSchema(name, existing, spec)
		if err != nil {
			return err
		}
		spec = merged
	}
	e.metadata.registerStructSchema(name, spec)
	return nil
}

func (e *Executor) RegisterInterfaceSchema(name string, spec *RuntimeInterfaceSpec) {
	if err := e.TryRegisterInterfaceSchema(name, spec); err != nil {
		panic(err)
	}
}

func (e *Executor) TryRegisterInterfaceSchema(name string, spec *RuntimeInterfaceSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if spec == nil {
		e.metadata.registerInterfaceSpec(name, nil)
		return nil
	}
	if err := CheckPublicFFIInterfaceSchema(name, spec); err != nil {
		return err
	}
	if existing, ok := e.metadata.interfacesByName[name]; ok {
		if err := CheckInterfaceSchemaCompatible(name, existing, spec); err != nil {
			return err
		}
	}
	e.metadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
	return nil
}

type SchemaConflictError struct {
	Kind     string
	Name     string
	Existing string
	New      string
}

func (e *SchemaConflictError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("ffi %s conflict for %s: existing=%s new=%s", e.Kind, e.Name, e.Existing, e.New)
}

func CheckRouteCompatible(name string, existing, next FFIRoute) error {
	if existing.Name != next.Name ||
		existing.MethodID != next.MethodID ||
		existing.Doc != next.Doc ||
		!SameRuntimeFuncSchema(existing.FuncSig, next.FuncSig) ||
		!sameRuntimeBridge(existing.Bridge, next.Bridge) {
		return &SchemaConflictError{
			Kind:     "route",
			Name:     name,
			Existing: fmt.Sprintf("method=%d sig=%s bridge=%s", existing.MethodID, runtimeRouteSignature(existing), runtimeBridgeIdentity(existing.Bridge)),
			New:      fmt.Sprintf("method=%d sig=%s bridge=%s", next.MethodID, runtimeRouteSignature(next), runtimeBridgeIdentity(next.Bridge)),
		}
	}
	return nil
}

func runtimeRouteSignature(route FFIRoute) string {
	if route.FuncSig != nil {
		return string(route.FuncSig.Spec)
	}
	return ""
}

func SameRuntimeFuncSchema(a, b *RuntimeFuncSig) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		if a.Spec != b.Spec || len(a.ParamModes) != len(b.ParamModes) {
			return false
		}
		for i := range a.ParamModes {
			if a.ParamModes[i] != b.ParamModes[i] {
				return false
			}
		}
		return true
	}
}

func SameRuntimeStructSchema(a, b *RuntimeStructSpec) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.TypeID == b.TypeID && a.Spec == b.Spec && a.Name == b.Name && a.Ownership == b.Ownership
	}
}

func MergeStructSchema(name string, existing, next *RuntimeStructSpec) (*RuntimeStructSpec, error) {
	switch {
	case existing == nil || next == nil:
		if existing == next {
			return next, nil
		}
	case SameRuntimeStructSchema(existing, next):
		return existing, nil
	}
	return nil, &SchemaConflictError{
		Kind:     "struct schema",
		Name:     name,
		Existing: runtimeStructSignature(existing),
		New:      runtimeStructSignature(next),
	}
}

func CheckInterfaceSchemaCompatible(name string, existing, next *RuntimeInterfaceSpec) error {
	if existing == nil || next == nil {
		if existing == next {
			return nil
		}
		return &SchemaConflictError{Kind: "interface schema", Name: name, Existing: runtimeInterfaceSignature(existing), New: runtimeInterfaceSignature(next)}
	}
	if existing.Spec == next.Spec {
		return nil
	}
	return &SchemaConflictError{Kind: "interface schema", Name: name, Existing: string(existing.Spec), New: string(next.Spec)}
}

func runtimeStructSignature(spec *RuntimeStructSpec) string {
	if spec == nil {
		return "<nil>"
	}
	return string(spec.Spec)
}

func runtimeInterfaceSignature(spec *RuntimeInterfaceSpec) string {
	if spec == nil {
		return "<nil>"
	}
	return string(spec.Spec)
}

func sameRuntimeBridge(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b)
}

func runtimeBridgeIdentity(bridge any) string {
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

func (e *Executor) RegisterConstant(name, val string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consts[name] = val
	if pkg, member := SplitExternalName(name); pkg != "" && member != "" && !strings.Contains(member, ".") {
		e.registerBoundPackageMemberLocked(pkg, &BoundFFIMember{
			Name:       member,
			Kind:       FFIMemberConst,
			ReadOnly:   true,
			ConstValue: val,
		})
	}
}

func (e *Executor) TryRegisterPackageValue(name string, spec *ValueSpec, value *Var) error {
	if name == "" {
		return errors.New("package value missing name")
	}
	if spec == nil {
		return errors.New("package value missing schema")
	}
	if err := CheckPublicFFIValueSpec(name, spec); err != nil {
		return err
	}
	if value == nil {
		return errors.New("package value missing value")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.packageValues == nil {
		e.packageValues = make(map[string]*BoundPackageValue)
	}
	if existing, ok := e.packageValues[name]; ok && existing != nil && existing.Spec != nil {
		if existing.Spec.Type.Raw != spec.Type.Raw {
			return &SchemaConflictError{
				Kind:     "package value",
				Name:     name,
				Existing: existing.Spec.Type.Raw.String(),
				New:      spec.Type.Raw.String(),
			}
		}
	}
	e.packageValues[name] = &BoundPackageValue{Name: name, Spec: spec, Value: value}
	if pkg, member := SplitExternalName(name); pkg != "" && member != "" && !strings.Contains(member, ".") {
		e.registerBoundPackageMemberLocked(pkg, &BoundFFIMember{
			Name:     member,
			Kind:     FFIMemberValue,
			Type:     spec.Type,
			ReadOnly: spec.ReadOnly,
			Value:    value,
		})
	}
	return nil
}
