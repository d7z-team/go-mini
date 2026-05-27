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
				if typ := v.RuntimeType(); !typ.IsEmpty() {
					out.SetRuntimeType(typ)
				} else {
					out.SetRuntimeType(MustParseRuntimeType(SpecModule))
				}
				return out
			} else if inter, ok := v.Ref.(*VMInterface); ok {
				out := &Var{VType: TypeInterface, Ref: inter}
				out.SetRuntimeType(v.RuntimeType())
				return out
			} else if errObj, ok := v.Ref.(error); ok {
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

func (e *Executor) slotPointerSlot(v *Var) (*Slot, bool) {
	if v == nil || v.VType != TypePointer || v.Ref == nil || v.Bridge != nil {
		return nil, false
	}
	target, ok := v.Ref.(*Slot)
	if !ok {
		return nil, false
	}
	return target, true
}

func (e *Executor) newSlotPointer(targetType RuntimeType, slot *Slot) *Var {
	if targetType.IsEmpty() {
		if slot != nil && !slot.Decl.IsEmpty() {
			targetType = slot.Decl
		} else if slot != nil && slot.Value != nil && !slot.Value.RuntimeType().IsEmpty() {
			targetType = slot.Value.RuntimeType()
		} else {
			targetType = MustParseRuntimeType(SpecAny)
		}
	}
	res := &Var{VType: TypePointer, Ref: slot}
	res.SetRawType(PtrType(targetType.Raw).String())
	return res
}

func (e *Executor) slotPointerTarget(v *Var) (*Var, bool) {
	slot, ok := e.slotPointerSlot(v)
	if !ok || slot == nil {
		return nil, false
	}
	return slot.Value, true
}

func (e *Executor) isSlotPointer(v *Var) bool {
	_, ok := e.slotPointerSlot(v)
	return ok
}

func (e *Executor) isOpaqueHandle(v *Var) bool {
	if v == nil || v.VType != TypeHostRef {
		return false
	}
	return v.Bridge != nil || v.Handle != 0
}

func (e *Executor) prepareValueForType(session *StackContext, v *Var, targetType RuntimeType) (*Var, error) {
	if targetType.IsEmpty() {
		return cloneVarForAssign(e.unwrapValue(v)), nil
	}
	if session != nil {
		return session.prepareAssignedValue(targetType, v)
	}
	ctx := &StackContext{Executor: e}
	return ctx.prepareAssignedValue(targetType, v)
}

func (e *Executor) validateAnyValue(v *Var) error {
	v = e.unwrapValue(v)
	if v == nil {
		return nil
	}
	switch v.VType {
	case TypeInt, TypeFloat, TypeString, TypeBool, TypeBytes:
		return nil
	case TypeError:
		if host := hostErrorFromError(goErrorFromVar(v)); host != nil && host.Handle != 0 {
			return errors.New("Any cannot carry host error handle")
		}
		return nil
	case TypeArray:
		arr, ok := v.Ref.(*VMArray)
		if !ok || arr == nil {
			return nil
		}
		for _, item := range arr.Snapshot() {
			if err := e.validateAnyValue(item); err != nil {
				return err
			}
		}
		return nil
	case TypeMap:
		m, ok := v.Ref.(*VMMap)
		if !ok || m == nil {
			return nil
		}
		for _, item := range m.Snapshot() {
			if err := e.validateAnyValue(item); err != nil {
				return err
			}
		}
		return nil
	case TypeStruct:
		st, ok := v.Ref.(*VMStruct)
		if !ok || st == nil {
			return nil
		}
		for _, field := range st.Fields {
			if field != nil {
				if err := e.validateAnyValue(field.Value); err != nil {
					return err
				}
			}
		}
		return nil
	case TypeAny:
		if inner, ok := v.Ref.(*Var); ok {
			return e.validateAnyValue(inner)
		}
		if v.Ref == nil {
			return nil
		}
		return fmt.Errorf("Any cannot carry host value %T", v.Ref)
	case TypePointer:
		return errors.New("Any cannot carry VM pointer")
	case TypeHostRef:
		return errors.New("Any cannot carry host reference")
	case TypeChannel:
		return errors.New("Any cannot carry channel")
	case TypeInterface:
		if iface, ok := v.Ref.(*VMInterface); ok && iface != nil && iface.Target != nil && iface.Target.Handle != 0 {
			return errors.New("Any cannot carry host interface handle")
		}
		return nil
	case TypeModule:
		return errors.New("Any cannot carry module")
	case TypeClosure:
		return nil
	default:
		return fmt.Errorf("Any cannot carry %s", v.VType)
	}
}

func (e *Executor) unwrapAddressVar(v *Var) *Var {
	return e.unwrapValue(v)
}

func (e *Executor) dereferenceValue(v *Var) (*Var, error) {
	v = e.unwrapValue(v)
	if v == nil {
		return nil, errors.New("dereference of nil pointer")
	}
	target, ok := e.slotPointerTarget(v)
	if !ok {
		return nil, &VMError{Message: fmt.Sprintf("cannot dereference type %v", v.VType), IsPanic: true}
	}
	return e.unwrapValue(target), nil
}
