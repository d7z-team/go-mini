package runtime

import (
	"errors"
	"fmt"
)

type resolvedAddress struct {
	load  func() (*Var, error)
	store func(*Var) error
}

func (e *Executor) resolveAddress(session *StackContext, lhs LHSValue) (*resolvedAddress, error) {
	switch desc := lhs.(type) {
	case nil:
		return &resolvedAddress{
			load: func() (*Var, error) { return nil, nil },
			store: func(*Var) error {
				return nil
			},
		}, nil
	case *LHSEnv:
		if desc.Sym.Kind != SymbolUnknown {
			return &resolvedAddress{
				load: func() (*Var, error) {
					return session.LoadSymbol(desc.Sym)
				},
				store: func(val *Var) error {
					return session.StoreSymbol(desc.Sym, val)
				},
			}, nil
		}
		return &resolvedAddress{
			load: func() (*Var, error) {
				return session.Load(desc.Name)
			},
			store: func(val *Var) error {
				return session.Store(desc.Name, val)
			},
		}, nil
	case *LHSIndex:
		obj := e.unwrapAddressVar(desc.Obj)
		idx := e.unwrapAddressVar(desc.Index)
		if obj == nil || idx == nil {
			return nil, errors.New("index access on nil")
		}
		switch obj.VType {
		case TypeArray:
			if idx.VType != TypeInt {
				return nil, &VMError{Message: fmt.Sprintf("array index must be Int64, got %v", idx.VType), IsPanic: true}
			}
			arr := arrayRef(obj)
			i := int(idx.I64)
			if _, ok := arr.Load(i); !ok {
				return nil, &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
			}
			elemType, ok := obj.RuntimeType().ReadArrayItemType()
			if !ok {
				elemType = MustParseRuntimeType(SpecAny)
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := arr.Load(i)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					prepared, err := e.prepareValueForType(session, val, elemType)
					if err != nil {
						return err
					}
					arr.Store(i, prepared)
					return nil
				},
			}, nil
		case TypeMap:
			m := mapRef(obj)
			keyType, valType, ok := obj.RuntimeType().GetMapKeyValueTypes()
			if !ok {
				return nil, &VMError{Message: fmt.Sprintf("invalid map runtime type: %s", obj.RuntimeType().Raw), IsPanic: true}
			}
			key, keyVar, err := e.comparableMapKey(idx, keyType)
			if err != nil {
				return nil, err
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := m.Load(key)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					if m == nil {
						return &VMError{Message: "assignment to nil map", IsPanic: true}
					}
					prepared, err := e.prepareValueForType(session, val, valType)
					if err != nil {
						return err
					}
					m.StoreWithKey(key, keyVar, prepared)
					return nil
				},
			}, nil
		}
		return nil, fmt.Errorf("type %v does not support index access", obj.VType)
	case *LHSMember:
		obj := e.unwrapAddressVar(desc.Obj)
		if obj == nil {
			return nil, errors.New("member access on nil object")
		}
		switch obj.VType {
		case TypeMap:
			m := mapRef(obj)
			keyType, valType, ok := obj.RuntimeType().GetMapKeyValueTypes()
			if !ok {
				return nil, &VMError{Message: fmt.Sprintf("invalid map runtime type: %s", obj.RuntimeType().Raw), IsPanic: true}
			}
			key, keyVar, err := e.comparableMapKey(NewString(desc.Property), keyType)
			if err != nil {
				return nil, err
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := m.Load(key)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					if m == nil {
						return &VMError{Message: "assignment to nil map", IsPanic: true}
					}
					prepared, err := e.prepareValueForType(session, val, valType)
					if err != nil {
						return err
					}
					m.StoreWithKey(key, keyVar, prepared)
					return nil
				},
			}, nil
		case TypeStruct:
			st := obj.Ref.(*VMStruct)
			field, ok := st.Field(desc.Property)
			if !ok {
				return nil, fmt.Errorf("unknown field %s", desc.Property)
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					return field.Value, nil
				},
				store: func(val *Var) error {
					return session.Assign(field, val)
				},
			}, nil
		case TypeModule:
			mod := obj.Ref.(*VMModule)
			return nil, &VMError{Message: fmt.Sprintf("module %s is read-only", mod.Name), IsPanic: true}
		case TypePointer:
			if obj.Ref == nil {
				return nil, errors.New("member access on nil pointer")
			}
			ref, ok := e.slotPointerTarget(obj)
			if !ok {
				return nil, errors.New("type Pointer does not support member access")
			}
			return e.resolveAddress(session, &LHSMember{Obj: ref, Property: desc.Property})
		}
		return nil, fmt.Errorf("type %v does not support member access", obj.VType)
	case *LHSDeref:
		target := e.unwrapAddressVar(desc.Target)
		if target == nil {
			return nil, errors.New("dereference of nil pointer")
		}
		if !e.isSlotPointer(target) {
			return nil, fmt.Errorf("type %v does not support dereference", target.VType)
		}
		return &resolvedAddress{
			load: func() (*Var, error) {
				return e.dereferenceValue(target)
			},
			store: func(val *Var) error {
				slot, _ := e.slotPointerSlot(target)
				return session.Assign(slot, val)
			},
		}, nil
	case *LHSSlice:
		obj := e.unwrapAddressVar(desc.Obj)
		if obj == nil {
			return nil, errors.New("slice access on nil object")
		}
		low, high, err := e.resolveSliceBoundsForAddress(obj, desc.Low, desc.High)
		if err != nil {
			return nil, err
		}
		switch obj.VType {
		case TypeBytes:
			return &resolvedAddress{
				load: func() (*Var, error) {
					return NewBytes(obj.B[low:high]), nil
				},
				store: func(val *Var) error {
					if val == nil || val.VType != TypeBytes {
						return fmt.Errorf("slice copy-back expects TypeBytes, got %v", valueTypeOf(val))
					}
					obj.B = spliceByteWindow(obj.B, low, high, val.B)
					return nil
				},
			}, nil
		case TypeArray:
			arr := arrayRef(obj)
			return &resolvedAddress{
				load: func() (*Var, error) {
					v := &Var{VType: TypeArray, Ref: &VMArray{Data: arr.Slice(low, high)}}
					v.SetRuntimeType(obj.RuntimeType())
					return v, nil
				},
				store: func(val *Var) error {
					if val == nil || val.VType != TypeArray {
						return fmt.Errorf("slice copy-back expects Array, got %v", valueTypeOf(val))
					}
					items := arrayRef(val).Snapshot()
					if !arr.ReplaceSlice(low, high, items) {
						return fmt.Errorf("slice bounds out of range [%d:%d]", low, high)
					}
					return nil
				},
			}, nil
		}
		return nil, fmt.Errorf("type %v does not support slice access", obj.VType)
	}
	return nil, &VMError{Message: fmt.Sprintf("unsupported LHS descriptor: %T", lhs), IsPanic: true}
}

func (e *Executor) resolvePointerSlot(session *StackContext, lhs LHSValue) (*Slot, error) {
	switch desc := lhs.(type) {
	case nil:
		return nil, errors.New("cannot take address of empty target")
	case *LHSEnv:
		if desc.Sym.Kind == SymbolBuiltin {
			return nil, fmt.Errorf("cannot take address of builtin %s", desc.Name)
		}
		var (
			slot *Slot
			err  error
		)
		if desc.Sym.Kind != SymbolUnknown {
			slot, err = session.CaptureSymbol(desc.Sym)
		} else {
			slot, err = session.CaptureVar(desc.Name)
		}
		if err != nil {
			return nil, err
		}
		if err := e.validatePointerSlot(slot); err != nil {
			return nil, err
		}
		return slot, nil
	case *LHSMember:
		obj := e.unwrapAddressVar(desc.Obj)
		if obj == nil {
			return nil, errors.New("member access on nil object")
		}
		switch obj.VType {
		case TypeStruct:
			st := obj.Ref.(*VMStruct)
			field, ok := st.Field(desc.Property)
			if !ok {
				return nil, fmt.Errorf("unknown field %s", desc.Property)
			}
			if err := e.validatePointerSlot(field); err != nil {
				return nil, err
			}
			return field, nil
		case TypePointer:
			target, ok := e.slotPointerTarget(obj)
			if !ok {
				return nil, errors.New("member access on nil pointer")
			}
			return e.resolvePointerSlot(session, &LHSMember{Obj: target, Property: desc.Property})
		default:
			return nil, fmt.Errorf("type %v is not addressable by member access", obj.VType)
		}
	case *LHSDeref:
		target := e.unwrapAddressVar(desc.Target)
		slot, ok := e.slotPointerSlot(target)
		if !ok || slot == nil {
			return nil, errors.New("cannot take address of non-pointer dereference")
		}
		if err := e.validatePointerSlot(slot); err != nil {
			return nil, err
		}
		return slot, nil
	case *LHSIndex:
		return nil, errors.New("index expression is not addressable")
	case *LHSSlice:
		return nil, errors.New("slice expression is not addressable")
	default:
		return nil, &VMError{Message: fmt.Sprintf("unsupported address target: %T", lhs), IsPanic: true}
	}
}

func (e *Executor) validatePointerSlot(slot *Slot) error {
	if slot == nil {
		return errors.New("cannot take address of nil slot")
	}
	if slot.Decl.IsHostRef() || e.runtimeTypeContainsHostOpaqueValue(slot.Decl, 0) {
		return fmt.Errorf("cannot take address of host identity or opaque host value: %s", slot.Decl.Raw)
	}
	if slot.Value != nil {
		v := e.unwrapAddressVar(slot.Value)
		if v != nil && (v.VType == TypeHostRef || e.runtimeTypeContainsHostOpaqueValue(v.RuntimeType(), 0)) {
			return fmt.Errorf("cannot take address of host identity or opaque host value: %s", v.RuntimeType().Raw)
		}
	}
	return nil
}

func (e *Executor) resolveSliceBoundsForAddress(obj, lowVar, highVar *Var) (int, int, error) {
	low, high := 0, -1
	if lowVar != nil {
		if lowVar.VType != TypeInt {
			return 0, 0, fmt.Errorf("slice low index must be Int64, got %v", lowVar.VType)
		}
		low = int(lowVar.I64)
	}
	if highVar != nil {
		if highVar.VType != TypeInt {
			return 0, 0, fmt.Errorf("slice high index must be Int64, got %v", highVar.VType)
		}
		high = int(highVar.I64)
	}
	var length int
	switch obj.VType {
	case TypeBytes:
		length = len(obj.B)
	case TypeArray:
		length = arrayRef(obj).Len()
	default:
		return 0, 0, fmt.Errorf("type %v does not support slice access", obj.VType)
	}
	if high == -1 {
		high = length
	}
	if low < 0 || high < low || high > length {
		return 0, 0, &VMError{Message: fmt.Sprintf("slice bounds out of range [%d:%d] with capacity %d", low, high, length), IsPanic: true}
	}
	return low, high, nil
}

func spliceByteWindow(base []byte, low, high int, replacement []byte) []byte {
	next := make([]byte, 0, low+len(replacement)+len(base)-high)
	next = append(next, base[:low]...)
	next = append(next, replacement...)
	next = append(next, base[high:]...)
	return next
}

func valueTypeOf(v *Var) string {
	if v == nil {
		return "<nil>"
	}
	return v.VType.String()
}

func (e *Executor) loadAddress(session *StackContext, lhs LHSValue) (*Var, error) {
	addr, err := e.resolveAddress(session, lhs)
	if err != nil {
		return nil, err
	}
	return addr.load()
}

func (e *Executor) storeAddress(session *StackContext, lhs LHSValue, val *Var) error {
	addr, err := e.resolveAddress(session, lhs)
	if err != nil {
		return err
	}
	return addr.store(val)
}

func (e *Executor) assignAddress(session *StackContext, lhs LHSValue, val *Var) error {
	return e.storeAddress(session, lhs, val)
}

func (e *Executor) updateAddress(session *StackContext, lhs LHSValue, op string) error {
	current, err := e.loadAddress(session, lhs)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	next := cloneVarForAssign(current)
	if op == "++" {
		next.I64++
	} else {
		next.I64--
	}
	return e.storeAddress(session, lhs, next)
}
