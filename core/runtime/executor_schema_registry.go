package runtime

import (
	"fmt"
	"reflect"
)

func (e *Executor) RegisterRoute(name string, route FFIRoute) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.routes[name]; ok {
		ensureCompatibleRuntimeRoute(name, existing, route)
	}
	e.routes[name] = route
}

func (e *Executor) RegisterStructSchema(name string, spec *RuntimeStructSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if spec == nil {
		delete(e.metadata.structsByName, name)
		return
	}
	if existing, ok := e.metadata.structsByName[name]; ok {
		merged, ok := mergeRuntimeStructSchema(existing, spec)
		if !ok {
			panic(fmt.Sprintf("ffi struct schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
		}
		spec = merged
	}
	e.metadata.registerStructSchema(name, spec)
}

func (e *Executor) RegisterInterfaceSchema(name string, spec *RuntimeInterfaceSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if spec == nil {
		e.metadata.registerInterfaceSpec(name, nil)
		return
	}
	if existing, ok := e.metadata.interfacesByName[name]; ok && existing != nil && existing.Spec != spec.Spec {
		panic(fmt.Sprintf("ffi interface schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
	}
	e.metadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
}

func ensureCompatibleRuntimeRoute(name string, existing, next FFIRoute) {
	if existing.Name != next.Name ||
		existing.MethodID != next.MethodID ||
		existing.Doc != next.Doc ||
		!sameRuntimeFuncSchema(existing.FuncSig, next.FuncSig) ||
		!sameRuntimeBridge(existing.Bridge, next.Bridge) {
		panic(fmt.Sprintf(
			"ffi route conflict for %s: existing(method=%d sig=%s bridge=%s) new(method=%d sig=%s bridge=%s)",
			name,
			existing.MethodID,
			runtimeRouteSignature(existing),
			runtimeBridgeIdentity(existing.Bridge),
			next.MethodID,
			runtimeRouteSignature(next),
			runtimeBridgeIdentity(next.Bridge),
		))
	}
}

func runtimeRouteSignature(route FFIRoute) string {
	if route.FuncSig != nil {
		return string(route.FuncSig.Spec)
	}
	return ""
}

func sameRuntimeFuncSchema(a, b *RuntimeFuncSig) bool {
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

func sameRuntimeStructSchema(a, b *RuntimeStructSpec) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.TypeID == b.TypeID && a.Spec == b.Spec && a.Name == b.Name && a.Ownership == b.Ownership
	}
}

func mergeRuntimeStructSchema(existing, next *RuntimeStructSpec) (*RuntimeStructSpec, bool) {
	switch {
	case existing == nil || next == nil:
		return next, existing == next
	case sameRuntimeStructSchema(existing, next):
		return existing, true
	}
	return nil, false
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
}
