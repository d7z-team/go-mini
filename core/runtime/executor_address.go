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
			arr := obj.Ref.(*VMArray)
			i := int(idx.I64)
			if _, ok := arr.Load(i); !ok {
				return nil, &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := arr.Load(i)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					arr.Store(i, val)
					return nil
				},
			}, nil
		case TypeMap:
			m := obj.Ref.(*VMMap)
			key, err := e.varToMapKey(idx)
			if err != nil {
				return nil, err
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := m.Load(key)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					m.Store(key, val)
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
			m := obj.Ref.(*VMMap)
			return &resolvedAddress{
				load: func() (*Var, error) {
					v, _ := m.Load(desc.Property)
					return e.unwrapAddressVar(v), nil
				},
				store: func(val *Var) error {
					m.Store(desc.Property, val)
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
			if mod.Context == nil {
				return nil, &VMError{Message: fmt.Sprintf("module %s is read-only", mod.Name), IsPanic: true}
			}
			return &resolvedAddress{
				load: func() (*Var, error) {
					if mod.Context.Shared != nil {
						if v, ok := mod.Context.Shared.LoadGlobal(desc.Property); ok {
							return v, nil
						}
					}
					return mod.Context.Load(desc.Property)
				},
				store: func(val *Var) error {
					if mod.Context.Shared != nil && mod.Context.Shared.HasGlobal(desc.Property) {
						return (&StackContext{
							Executor: mod.Context.Executor,
							Shared:   mod.Context.Shared,
							Stack:    mod.Context.Stack,
						}).StoreSymbol(SymbolRef{Name: desc.Property, Kind: SymbolGlobal, Slot: -1}, val)
					}
					return mod.Context.Store(desc.Property, val)
				},
			}, nil
		case TypeHandle:
			if obj.Ref == nil {
				return nil, errors.New("member access on nil pointer")
			}
			ref, ok := e.vmPointerTarget(obj)
			if !ok {
				return nil, errors.New("type Handle does not support member access")
			}
			return e.resolveAddress(session, &LHSMember{Obj: ref, Property: desc.Property})
		}
		return nil, fmt.Errorf("type %v does not support member access", obj.VType)
	case *LHSDeref:
		target := e.unwrapAddressVar(desc.Target)
		if target == nil {
			return nil, errors.New("dereference of nil pointer")
		}
		if !e.isVMPointer(target) {
			return nil, fmt.Errorf("type %v does not support dereference", target.VType)
		}
		return &resolvedAddress{
			load: func() (*Var, error) {
				return e.dereferenceValue(target)
			},
			store: func(val *Var) error {
				slot, _ := e.vmPointerSlot(target)
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
			arr := obj.Ref.(*VMArray)
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
					items := val.Ref.(*VMArray).Snapshot()
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
		length = obj.Ref.(*VMArray).Len()
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
