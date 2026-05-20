package runtime

import (
	"errors"
	"fmt"
)

func (e *Executor) unwrapValue(v *Var) *Var {
	for v != nil {
		switch v.VType {
		case TypeAny:
			if inner, ok := v.Ref.(*Var); ok {
				v = inner
			} else if m, ok := v.Ref.(*VMMap); ok {
				out := &Var{VType: TypeMap, Ref: m}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if st, ok := v.Ref.(*VMStruct); ok {
				out := &Var{VType: TypeStruct, Ref: st}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if arr, ok := v.Ref.(*VMArray); ok {
				out := &Var{VType: TypeArray, Ref: arr}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if mod, ok := v.Ref.(*VMModule); ok {
				out := &Var{VType: TypeModule, Ref: mod}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if inter, ok := v.Ref.(*VMInterface); ok {
				out := &Var{VType: TypeInterface, Ref: inter}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if errObj, ok := v.Ref.(*VMError); ok {
				out := &Var{VType: TypeError, Ref: errObj}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else {
				return v
			}
		default:
			return v
		}
	}
	return nil
}

func (e *Executor) vmPointerSlot(v *Var) (*Slot, bool) {
	if v == nil || v.VType != TypeHandle || v.Ref == nil || v.Bridge != nil {
		return nil, false
	}
	target, ok := v.Ref.(*Slot)
	if !ok {
		return nil, false
	}
	return target, true
}

func (e *Executor) vmPointerTarget(v *Var) (*Var, bool) {
	slot, ok := e.vmPointerSlot(v)
	if !ok || slot == nil {
		return nil, false
	}
	return slot.Value, true
}

func (e *Executor) isVMPointer(v *Var) bool {
	_, ok := e.vmPointerSlot(v)
	return ok
}

func (e *Executor) isOpaqueHandle(v *Var) bool {
	if v == nil || v.VType != TypeHostRef {
		return false
	}
	return v.Bridge != nil || v.Handle != 0
}

func (e *Executor) normalizeTypedValue(v *Var, targetType RuntimeType) *Var {
	v = e.unwrapValue(v)
	if v == nil {
		return nil
	}
	runtimeType := v.RuntimeType()
	if runtimeType.IsEmpty() || runtimeType.IsAny() {
		v.SetRuntimeType(targetType)
	}
	return v
}

func (e *Executor) unwrapAddressVar(v *Var) *Var {
	return e.unwrapValue(v)
}

func (e *Executor) dereferenceValue(v *Var) (*Var, error) {
	v = e.unwrapValue(v)
	if v == nil {
		return nil, errors.New("dereference of nil pointer")
	}
	target, ok := e.vmPointerTarget(v)
	if !ok {
		return nil, &VMError{Message: fmt.Sprintf("cannot dereference type %v", v.VType), IsPanic: true}
	}
	return e.unwrapValue(target), nil
}
