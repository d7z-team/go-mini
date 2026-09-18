package runtime

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func anyWrapper(v *Var) (*Var, bool, bool) {
	if v == nil || v.VType != TypeAny {
		return v, false, false
	}
	if v.Ref == nil {
		return nil, true, true
	}
	inner, _ := v.Ref.(*Var)
	return inner, true, false
}

func comparisonNilOperand(original, unwrapped *Var) bool {
	if original != nil && original.VType == TypeAny {
		return original.Ref == nil
	}
	return isNilValue(unwrapped)
}

func escapeMapKeyString(s string) string {
	return strconv.Itoa(len(s)) + ":" + s
}

func comparableRuntimeType(v *Var) RuntimeType {
	if v == nil {
		return RuntimeType{}
	}
	if !v.RuntimeType().IsEmpty() {
		return v.RuntimeType()
	}
	return runtimeTypeForAssignment(v)
}

func isEqualityComparableRuntimeType(t RuntimeType) bool {
	if t.IsEmpty() || t.IsAny() {
		return false
	}
	switch {
	case t.IsBool(), t.IsString(), t.IsNumeric():
		return true
	case t.IsPtr(), t.IsHostRef(), t.IsChan():
		return true
	case t.Raw == SpecError:
		return true
	case t.IsMap():
		return true
	case t.IsArray():
		return true
	case t.Kind == RuntimeTypeStruct:
		for _, field := range t.Fields {
			if !isEqualityComparableRuntimeType(field.TypeInfo) {
				return false
			}
		}
		return true
	case t.IsInterface():
		return true
	}
	return t.Raw.IsModule() || t.Raw.IsClosure()
}

func isMapKeyRuntimeType(t RuntimeType) bool {
	if t.IsEmpty() || t.IsAny() {
		return false
	}
	switch {
	case t.IsBool(), t.IsString(), t.IsNumeric():
		return true
	case t.IsPtr(), t.IsHostRef(), t.IsChan():
		return true
	case t.Raw == SpecError:
		return true
	case t.IsArray():
		return t.Elem != nil && isMapKeyRuntimeType(*t.Elem)
	case t.Kind == RuntimeTypeStruct:
		for _, field := range t.Fields {
			if !isMapKeyRuntimeType(field.TypeInfo) {
				return false
			}
		}
		return true
	case t.IsInterface():
		return true
	}
	return false
}

func comparableValuesEqual(left, right *Var) (bool, error) {
	if left == nil || right == nil {
		return left == right, nil
	}
	if left.VType == TypeArray || right.VType == TypeArray {
		if left.VType != right.VType {
			return false, fmt.Errorf("type mismatch: %s vs %s", runtimeTypeForAssignment(left).Raw, runtimeTypeForAssignment(right).Raw)
		}
		return left.Ref == right.Ref, nil
	}
	if left.VType == TypeMap || right.VType == TypeMap {
		if left.VType != right.VType {
			return false, fmt.Errorf("type mismatch: %s vs %s", runtimeTypeForAssignment(left).Raw, runtimeTypeForAssignment(right).Raw)
		}
		return left.Ref == right.Ref, nil
	}
	if left.VType == TypeModule || right.VType == TypeModule || left.VType == TypeClosure || right.VType == TypeClosure {
		if left.VType != right.VType {
			return false, fmt.Errorf("type mismatch: %s vs %s", runtimeTypeForAssignment(left).Raw, runtimeTypeForAssignment(right).Raw)
		}
		return left.Ref == right.Ref, nil
	}
	if left.VType == TypeInterface {
		iface, _ := left.Ref.(*VMInterface)
		if iface == nil {
			left = nil
		} else {
			left = iface.Target
		}
	}
	if right.VType == TypeInterface {
		iface, _ := right.Ref.(*VMInterface)
		if iface == nil {
			right = nil
		} else {
			right = iface.Target
		}
	}
	if left == nil || right == nil {
		return left == right, nil
	}
	if left.VType != right.VType {
		return false, fmt.Errorf("type mismatch: %s vs %s", runtimeTypeForAssignment(left).Raw, runtimeTypeForAssignment(right).Raw)
	}
	lk, err := comparableValueString(left)
	if err != nil {
		return false, err
	}
	rk, err := comparableValueString(right)
	if err != nil {
		return false, err
	}
	return lk == rk, nil
}

func comparableValueString(v *Var) (string, error) {
	return comparableValueStringSeen(v, &comparableSeen{})
}

type comparableSeen struct {
	vars       map[*Var]struct{}
	arrays     map[*VMArray]struct{}
	structs    map[*VMStruct]struct{}
	interfaces map[*VMInterface]struct{}
}

func comparableValueStringSeen(v *Var, seen *comparableSeen) (string, error) {
	inner, wrappedAny, nilAny := anyWrapper(v)
	if wrappedAny {
		if nilAny {
			return "nil", nil
		}
		return comparableValueStringSeen(inner, seen)
	}
	if v == nil {
		return "nil", nil
	}
	if seen != nil {
		if seen.vars == nil {
			seen.vars = make(map[*Var]struct{})
		}
		if _, ok := seen.vars[v]; ok {
			return "", errors.New("cyclic value is not comparable")
		}
		seen.vars[v] = struct{}{}
		defer delete(seen.vars, v)
	}
	if v.VType == TypeInterface {
		iface, _ := v.Ref.(*VMInterface)
		if iface == nil || iface.Target == nil {
			return "nil", nil
		}
		if seen != nil && iface != nil {
			if seen.interfaces == nil {
				seen.interfaces = make(map[*VMInterface]struct{})
			}
			if _, ok := seen.interfaces[iface]; ok {
				return "", errors.New("cyclic value is not comparable")
			}
			seen.interfaces[iface] = struct{}{}
			defer delete(seen.interfaces, iface)
		}
		return comparableValueStringSeen(iface.Target, seen)
	}
	rt := comparableRuntimeType(v)
	switch v.VType {
	case TypeInt:
		return "i#" + rt.Raw.String() + "#" + strconv.FormatInt(v.I64, 10), nil
	case TypeFloat:
		return "f#" + rt.Raw.String() + "#" + strconv.FormatUint(math.Float64bits(v.F64), 16), nil
	case TypeString:
		return "s#" + rt.Raw.String() + "#" + escapeMapKeyString(v.Str), nil
	case TypeBool:
		if v.Bool {
			return "b#" + rt.Raw.String() + "#1", nil
		}
		return "b#" + rt.Raw.String() + "#0", nil
	case TypePointer:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		if slot, ok := v.Ref.(*Slot); ok {
			return fmt.Sprintf("ptr#%s#%p", rt.Raw, slot), nil
		}
		return fmt.Sprintf("ptr#%s#nil", rt.Raw), nil
	case TypeHostRef:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("host#%s#%d#%p", rt.Raw, v.Handle, v.Bridge), nil
	case TypeChannel:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("chan#%s#%p", rt.Raw, v.Ref), nil
	case TypeError:
		errVal := goErrorFromVar(v)
		if errVal == nil {
			return "err#nil", nil
		}
		if host := hostErrorFromError(errVal); host != nil && host.Handle != 0 {
			return fmt.Sprintf("err#host#%s#%d", runtimeBridgeIdentity(host.Bridge), host.Handle), nil
		}
		if id, ok := vmErrorIdentity(errVal); ok {
			return "err#" + id, nil
		}
		return "", fmt.Errorf("error %T has no VM identity", errVal)
	case TypeArray:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("arr#%s#%p", rt.Raw, v.Ref), nil
	case TypeMap:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("map#%s#%p", rt.Raw, v.Ref), nil
	case TypeModule:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("module#%s#%p", rt.Raw, v.Ref), nil
	case TypeClosure:
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("closure#%s#%p", rt.Raw, v.Ref), nil
	case TypeStruct:
		return comparableStructStringSeen(v, rt, seen, false)
	default:
		return "", fmt.Errorf("%s is not comparable", v.VType)
	}
}

func comparableStructStringSeen(v *Var, rt RuntimeType, seen *comparableSeen, mapKey bool) (string, error) {
	if mapKey {
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
	} else if !isEqualityComparableRuntimeType(rt) {
		return "", fmt.Errorf("%s is not comparable", rt.Raw)
	}
	st, _ := v.Ref.(*VMStruct)
	if st == nil {
		return "struct#" + rt.Raw.String() + "#nil", nil
	}
	if seen != nil {
		if seen.structs == nil {
			seen.structs = make(map[*VMStruct]struct{})
		}
		if _, ok := seen.structs[st]; ok {
			return "", errors.New("cyclic value is not comparable")
		}
		seen.structs[st] = struct{}{}
		defer delete(seen.structs, st)
	}
	parts := make([]string, 0, len(st.Fields))
	for i, field := range st.Fields {
		if field == nil {
			parts = append(parts, strconv.Itoa(i)+"=nil")
			continue
		}
		var (
			part string
			err  error
		)
		if mapKey {
			part, err = comparableMapKeyStringSeen(field.Value, seen)
		} else {
			part, err = comparableValueStringSeen(field.Value, seen)
		}
		if err != nil {
			return "", err
		}
		parts = append(parts, strconv.Itoa(i)+"="+part)
	}
	return "struct#" + rt.Raw.String() + "#" + strings.Join(parts, ";"), nil
}

func comparableMapKeyString(v *Var) (string, error) {
	return comparableMapKeyStringSeen(v, &comparableSeen{})
}

func comparableMapKeyStringSeen(v *Var, seen *comparableSeen) (string, error) {
	inner, wrappedAny, nilAny := anyWrapper(v)
	if wrappedAny {
		if nilAny {
			return "nil", nil
		}
		return comparableMapKeyStringSeen(inner, seen)
	}
	if v == nil {
		return "nil", nil
	}
	if seen != nil {
		if seen.vars == nil {
			seen.vars = make(map[*Var]struct{})
		}
		if _, ok := seen.vars[v]; ok {
			return "", errors.New("cyclic value is not comparable")
		}
		seen.vars[v] = struct{}{}
		defer delete(seen.vars, v)
	}
	if v.VType == TypeInterface {
		iface, _ := v.Ref.(*VMInterface)
		if iface == nil || iface.Target == nil {
			return "nil", nil
		}
		if seen != nil && iface != nil {
			if seen.interfaces == nil {
				seen.interfaces = make(map[*VMInterface]struct{})
			}
			if _, ok := seen.interfaces[iface]; ok {
				return "", errors.New("cyclic value is not comparable")
			}
			seen.interfaces[iface] = struct{}{}
			defer delete(seen.interfaces, iface)
		}
		return comparableMapKeyStringSeen(iface.Target, seen)
	}
	rt := comparableRuntimeType(v)
	switch v.VType {
	case TypeInt:
		return "i#" + rt.Raw.String() + "#" + strconv.FormatInt(v.I64, 10), nil
	case TypeFloat:
		return "f#" + rt.Raw.String() + "#" + strconv.FormatUint(math.Float64bits(v.F64), 16), nil
	case TypeString:
		return "s#" + rt.Raw.String() + "#" + escapeMapKeyString(v.Str), nil
	case TypeBool:
		if v.Bool {
			return "b#" + rt.Raw.String() + "#1", nil
		}
		return "b#" + rt.Raw.String() + "#0", nil
	case TypePointer:
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		if slot, ok := v.Ref.(*Slot); ok {
			return fmt.Sprintf("ptr#%s#%p", rt.Raw, slot), nil
		}
		return fmt.Sprintf("ptr#%s#nil", rt.Raw), nil
	case TypeHostRef:
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("host#%s#%d#%p", rt.Raw, v.Handle, v.Bridge), nil
	case TypeChannel:
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		return fmt.Sprintf("chan#%s#%p", rt.Raw, v.Ref), nil
	case TypeError:
		errVal := goErrorFromVar(v)
		if errVal == nil {
			return "err#nil", nil
		}
		if host := hostErrorFromError(errVal); host != nil && host.Handle != 0 {
			return fmt.Sprintf("err#host#%s#%d", runtimeBridgeIdentity(host.Bridge), host.Handle), nil
		}
		if id, ok := vmErrorIdentity(errVal); ok {
			return "err#" + id, nil
		}
		return "", fmt.Errorf("error %T has no VM identity", errVal)
	case TypeArray:
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		arr := arrayRef(v)
		if arr == nil {
			return "arr#" + rt.Raw.String() + "#nil", nil
		}
		if arr != nil && seen != nil {
			if seen.arrays == nil {
				seen.arrays = make(map[*VMArray]struct{})
			}
			if _, ok := seen.arrays[arr]; ok {
				return "", errors.New("cyclic value is not comparable")
			}
			seen.arrays[arr] = struct{}{}
			defer delete(seen.arrays, arr)
		}
		items := arr.Snapshot()
		parts := make([]string, len(items))
		for i, item := range items {
			part, err := comparableMapKeyStringSeen(item, seen)
			if err != nil {
				return "", err
			}
			parts[i] = part
		}
		return "arr#" + rt.Raw.String() + "#" + strings.Join(parts, "|"), nil
	case TypeStruct:
		return comparableStructStringSeen(v, rt, seen, true)
	default:
		return "", fmt.Errorf("%s is not comparable", v.VType)
	}
}

func (e *Executor) comparableMapKey(v *Var, keyType RuntimeType) (string, *Var, error) {
	if keyType.IsAny() {
		if err := e.validateAnyValue(v); err != nil {
			return "", nil, err
		}
		key, err := comparableMapKeyString(v)
		if err != nil {
			return "", nil, err
		}
		return key, cloneVarForAssign(v), nil
	}
	prepared, err := e.prepareValueForType(nil, v, keyType)
	if err != nil {
		return "", nil, err
	}
	if keyType.IsString() {
		if prepared == nil {
			return "", nil, errors.New("String map key cannot be nil")
		}
		return prepared.Str, cloneVarForAssign(prepared), nil
	}
	key, err := comparableMapKeyString(prepared)
	if err != nil {
		return "", nil, err
	}
	return key, cloneVarForAssign(prepared), nil
}

func (e *Executor) ffiMapStringKey(v *Var) (string, error) {
	if v == nil {
		return "", errors.New("ffi map key cannot be nil")
	}
	if v.VType != TypeString {
		return "", fmt.Errorf("ffi map key must be String, got %s", runtimeTypeForAssignment(v).Raw)
	}
	return v.Str, nil
}
