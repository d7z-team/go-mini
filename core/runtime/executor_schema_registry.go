package runtime

import (
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

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
	return runtimeBridgeIdentity(a) == runtimeBridgeIdentity(b)
}

func runtimeBridgeIdentity(bridge any) string {
	if bridge == nil {
		return "<nil>"
	}
	if identified, ok := bridge.(ffigo.BridgeIdentity); ok {
		id := strings.TrimSpace(identified.BridgeID())
		if id != "" {
			return id
		}
	}
	return "<missing bridge identity>"
}

func (e *Executor) RegisterModuleHash(path, hash string) {
	path = strings.TrimSpace(path)
	if path == "" || hash == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.moduleHashes == nil {
		e.moduleHashes = make(map[string]string)
	}
	e.moduleHashes[path] = hash
}
