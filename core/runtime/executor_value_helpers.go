package runtime

import (
	"errors"
	"fmt"
)

type anyValidationMode uint8

const (
	anyValidationVM anyValidationMode = iota
	anyValidationFFI
)

func (e *Executor) unwrapValue(v *Var) *Var {
	for v != nil {
		if v.VType != TypeAny {
			return v
		}
		inner, ok := v.Ref.(*Var)
		if !ok || inner == nil {
			return v
		}
		v = inner
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
	return e.validateAnyValueMode(v, anyValidationVM)
}

func (e *Executor) validateFFIAnyValue(v *Var) error {
	return e.validateAnyValueMode(v, anyValidationFFI)
}

func (e *Executor) validateAnyValueMode(v *Var, mode anyValidationMode) error {
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
			if err := e.validateAnyValueMode(item, mode); err != nil {
				return err
			}
		}
		return nil
	case TypeMap:
		m, ok := v.Ref.(*VMMap)
		if !ok || m == nil {
			return nil
		}
		for _, entry := range m.Entries() {
			key := entry.Key
			if key == nil {
				key = NewString(entry.Encoded)
			}
			if mode == anyValidationFFI {
				if _, err := e.ffiMapStringKey(key); err != nil {
					return err
				}
			} else if err := e.validateAnyValueMode(key, mode); err != nil {
				return err
			}
			if err := e.validateAnyValueMode(entry.Value, mode); err != nil {
				return err
			}
		}
		return nil
	case TypeStruct:
		st, ok := v.Ref.(*VMStruct)
		if !ok || st == nil {
			return nil
		}
		if mode == anyValidationFFI && st.Spec != nil && st.Spec.Ownership == StructOwnershipHostOpaque {
			return errors.New("FFI Any cannot carry host opaque struct")
		}
		for _, field := range st.Fields {
			if field != nil {
				if err := e.validateAnyValueMode(field.Value, mode); err != nil {
					return err
				}
			}
		}
		return nil
	case TypeAny:
		if inner, ok := v.Ref.(*Var); ok {
			return e.validateAnyValueMode(inner, mode)
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
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry interface")
		}
		if iface, ok := v.Ref.(*VMInterface); ok && iface != nil && iface.Target != nil {
			if iface.Target.Handle != 0 {
				return errors.New("Any cannot carry host interface handle")
			}
			return e.validateAnyValueMode(iface.Target, mode)
		}
		return nil
	case TypeModule:
		return errors.New("Any cannot carry module")
	case TypeClosure:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry closure")
		}
		return nil
	default:
		return fmt.Errorf("Any cannot carry %s", v.VType)
	}
}

func arrayRef(v *Var) *VMArray {
	if v == nil || v.VType != TypeArray {
		return nil
	}
	arr, _ := v.Ref.(*VMArray)
	return arr
}

func mapRef(v *Var) *VMMap {
	if v == nil || v.VType != TypeMap {
		return nil
	}
	m, _ := v.Ref.(*VMMap)
	return m
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
