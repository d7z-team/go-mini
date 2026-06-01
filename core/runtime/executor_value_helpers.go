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

type anyValidationSeen struct {
	vars       map[*Var]struct{}
	arrays     map[*VMArray]struct{}
	maps       map[*VMMap]struct{}
	structs    map[*VMStruct]struct{}
	interfaces map[*VMInterface]struct{}
}

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

func (e *Executor) byteSliceFromArray(v *Var) ([]byte, error) {
	v = e.unwrapValue(v)
	if v == nil {
		return nil, nil
	}
	if v.VType != TypeArray {
		return nil, fmt.Errorf("expected Array<Byte>, got %s", runtimeTypeForAssignment(v).Raw)
	}
	if !isByteArrayType(v.RuntimeType()) {
		return nil, fmt.Errorf("expected Array<Byte>, got %s", v.RuntimeType().Raw)
	}
	arr := arrayRef(v)
	if arr == nil {
		return nil, nil
	}
	items := arr.Snapshot()
	out := make([]byte, len(items))
	for i, item := range items {
		item = e.unwrapValue(item)
		if item == nil || item.VType != TypeInt {
			return nil, fmt.Errorf("byte array item %d is not Byte", i)
		}
		if runtimeTypeForAssignment(item).Raw != SpecByte {
			return nil, fmt.Errorf("byte array item %d is %s, not Byte", i, runtimeTypeForAssignment(item).Raw)
		}
		if err := checkIntegerSubtypeRange(SpecByte, item.I64); err != nil {
			return nil, fmt.Errorf("byte array item %d: %w", i, err)
		}
		out[i] = byte(item.I64)
	}
	return out, nil
}

func (e *Executor) stringFromRuneArray(v *Var) (string, error) {
	v = e.unwrapValue(v)
	if v == nil {
		return "", nil
	}
	if v.VType != TypeArray {
		return "", fmt.Errorf("expected Array<Rune>, got %s", runtimeTypeForAssignment(v).Raw)
	}
	if !isRuneArrayType(v.RuntimeType()) {
		return "", fmt.Errorf("expected Array<Rune>, got %s", v.RuntimeType().Raw)
	}
	arr := arrayRef(v)
	if arr == nil {
		return "", nil
	}
	items := arr.Snapshot()
	out := make([]rune, len(items))
	for i, item := range items {
		item = e.unwrapValue(item)
		if item == nil || item.VType != TypeInt {
			return "", fmt.Errorf("rune array item %d is not Rune", i)
		}
		if runtimeTypeForAssignment(item).Raw != SpecRune {
			return "", fmt.Errorf("rune array item %d is %s, not Rune", i, runtimeTypeForAssignment(item).Raw)
		}
		out[i] = rune(item.I64)
	}
	return string(out), nil
}

func (e *Executor) validateAnyValue(v *Var) error {
	return e.validateAnyValueMode(v, anyValidationVM)
}

func (e *Executor) validateFFIAnyValue(v *Var) error {
	return e.validateAnyValueMode(v, anyValidationFFI)
}

func (e *Executor) validateAnyValueMode(v *Var, mode anyValidationMode) error {
	return e.validateAnyValueModeSeen(v, mode, &anyValidationSeen{})
}

func (e *Executor) validateAnyValueModeSeen(v *Var, mode anyValidationMode, seen *anyValidationSeen) error {
	v = e.unwrapValue(v)
	if v == nil {
		return nil
	}
	if seen != nil {
		if _, ok := seen.vars[v]; ok {
			if mode == anyValidationFFI {
				return errors.New("FFI Any cannot carry cyclic value")
			}
			return nil
		}
		if seen.vars == nil {
			seen.vars = make(map[*Var]struct{})
		}
		seen.vars[v] = struct{}{}
		defer delete(seen.vars, v)
	}
	switch v.VType {
	case TypeInt, TypeFloat, TypeString, TypeBool:
		return nil
	case TypeError:
		if mode == anyValidationFFI {
			if host := hostErrorFromError(goErrorFromVar(v)); host != nil && host.Handle != 0 {
				return errors.New("FFI Any cannot carry host error handle")
			}
		}
		return nil
	case TypeArray:
		arr, ok := v.Ref.(*VMArray)
		if !ok || arr == nil {
			return nil
		}
		if seen != nil {
			if _, ok := seen.arrays[arr]; ok {
				if mode == anyValidationFFI {
					return errors.New("FFI Any cannot carry cyclic value")
				}
				return nil
			}
			if seen.arrays == nil {
				seen.arrays = make(map[*VMArray]struct{})
			}
			seen.arrays[arr] = struct{}{}
			defer delete(seen.arrays, arr)
		}
		for _, item := range arr.Snapshot() {
			if err := e.validateAnyValueModeSeen(item, mode, seen); err != nil {
				return err
			}
		}
		return nil
	case TypeMap:
		m, ok := v.Ref.(*VMMap)
		if !ok || m == nil {
			return nil
		}
		if seen != nil {
			if _, ok := seen.maps[m]; ok {
				if mode == anyValidationFFI {
					return errors.New("FFI Any cannot carry cyclic value")
				}
				return nil
			}
			if seen.maps == nil {
				seen.maps = make(map[*VMMap]struct{})
			}
			seen.maps[m] = struct{}{}
			defer delete(seen.maps, m)
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
			} else if err := e.validateAnyValueModeSeen(key, mode, seen); err != nil {
				return err
			}
			if err := e.validateAnyValueModeSeen(entry.Value, mode, seen); err != nil {
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
		if seen != nil {
			if _, ok := seen.structs[st]; ok {
				if mode == anyValidationFFI {
					return errors.New("FFI Any cannot carry cyclic value")
				}
				return nil
			}
			if seen.structs == nil {
				seen.structs = make(map[*VMStruct]struct{})
			}
			seen.structs[st] = struct{}{}
			defer delete(seen.structs, st)
		}
		for _, field := range st.Fields {
			if field != nil {
				if err := e.validateAnyValueModeSeen(field.Value, mode, seen); err != nil {
					return err
				}
			}
		}
		return nil
	case TypeAny:
		switch ref := v.Ref.(type) {
		case *Var:
			inner := ref
			return e.validateAnyValueModeSeen(inner, mode, seen)
		case nil:
			return nil
		case FFIRoute:
			if mode == anyValidationFFI {
				return errors.New("FFI Any cannot carry closure")
			}
			return nil
		case *RuntimeStructSpec, *RuntimeInterfaceSpec:
			if mode == anyValidationFFI {
				return errors.New("FFI Any cannot carry metadata")
			}
			return nil
		default:
			return fmt.Errorf("Any cannot carry host value %T", v.Ref)
		}
	case TypePointer:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry VM pointer")
		}
		return nil
	case TypeHostRef:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry host reference")
		}
		return nil
	case TypeChannel:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry channel")
		}
		return nil
	case TypeInterface:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry interface")
		}
		if iface, ok := v.Ref.(*VMInterface); ok && iface != nil && iface.Target != nil {
			if seen != nil {
				if _, ok := seen.interfaces[iface]; ok {
					return nil
				}
				if seen.interfaces == nil {
					seen.interfaces = make(map[*VMInterface]struct{})
				}
				seen.interfaces[iface] = struct{}{}
				defer delete(seen.interfaces, iface)
			}
			return e.validateAnyValueModeSeen(iface.Target, mode, seen)
		}
		return nil
	case TypeModule:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry module")
		}
		return nil
	case TypeClosure:
		if mode == anyValidationFFI {
			return errors.New("FFI Any cannot carry closure")
		}
		return nil
	default:
		if mode == anyValidationFFI {
			return fmt.Errorf("FFI Any cannot carry %s", v.VType)
		}
		return nil
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
