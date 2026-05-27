package runtime

import (
	"errors"
	"fmt"
	"math"
	"reflect"
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
	inner, wrappedAny, nilAny := anyWrapper(v)
	if wrappedAny {
		if nilAny {
			return "nil", nil
		}
		return comparableValueString(inner)
	}
	if v == nil {
		return "nil", nil
	}
	if v.VType == TypeInterface {
		iface, _ := v.Ref.(*VMInterface)
		if iface == nil || iface.Target == nil {
			return "nil", nil
		}
		return comparableValueString(iface.Target)
	}
	rt := comparableRuntimeType(v)
	switch v.VType {
	case TypeInt:
		return "i#" + strconv.FormatInt(v.I64, 10), nil
	case TypeFloat:
		return "f#" + strconv.FormatUint(math.Float64bits(v.F64), 16), nil
	case TypeString:
		return "s#" + escapeMapKeyString(v.Str), nil
	case TypeBool:
		if v.Bool {
			return "b#1", nil
		}
		return "b#0", nil
	case TypeBytes:
		return "", errors.New("TypeBytes is not comparable")
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
			return fmt.Sprintf("err#host#%d#%p", host.Handle, host.Bridge), nil
		}
		rv := reflect.ValueOf(errVal)
		if !rv.IsValid() {
			return "err#nil", nil
		}
		if rv.Kind() == reflect.Pointer || rv.Kind() == reflect.UnsafePointer {
			return fmt.Sprintf("err#ptr#%T#0x%x", errVal, rv.Pointer()), nil
		}
		if !rv.Type().Comparable() {
			return "", fmt.Errorf("error %T is not comparable", errVal)
		}
		return fmt.Sprintf("err#val#%T#%#v", errVal, errVal), nil
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
		if !isEqualityComparableRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		st, _ := v.Ref.(*VMStruct)
		if st == nil {
			return "struct#" + rt.Raw.String() + "#nil", nil
		}
		parts := make([]string, 0, len(st.Fields))
		for i, field := range st.Fields {
			if field == nil {
				parts = append(parts, strconv.Itoa(i)+"=nil")
				continue
			}
			part, err := comparableValueString(field.Value)
			if err != nil {
				return "", err
			}
			parts = append(parts, strconv.Itoa(i)+"="+part)
		}
		return "struct#" + rt.Raw.String() + "#" + strings.Join(parts, ";"), nil
	default:
		return "", fmt.Errorf("%s is not comparable", v.VType)
	}
}

func comparableMapKeyString(v *Var) (string, error) {
	inner, wrappedAny, nilAny := anyWrapper(v)
	if wrappedAny {
		if nilAny {
			return "nil", nil
		}
		return comparableMapKeyString(inner)
	}
	if v == nil {
		return "nil", nil
	}
	if v.VType == TypeInterface {
		iface, _ := v.Ref.(*VMInterface)
		if iface == nil || iface.Target == nil {
			return "nil", nil
		}
		return comparableMapKeyString(iface.Target)
	}
	rt := comparableRuntimeType(v)
	switch v.VType {
	case TypeInt:
		return "i#" + strconv.FormatInt(v.I64, 10), nil
	case TypeFloat:
		return "f#" + strconv.FormatUint(math.Float64bits(v.F64), 16), nil
	case TypeString:
		return "s#" + escapeMapKeyString(v.Str), nil
	case TypeBool:
		if v.Bool {
			return "b#1", nil
		}
		return "b#0", nil
	case TypeBytes:
		return "", errors.New("TypeBytes is not comparable")
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
		return comparableValueString(v)
	case TypeArray:
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		arr := arrayRef(v)
		items := arr.Snapshot()
		parts := make([]string, len(items))
		for i, item := range items {
			part, err := comparableMapKeyString(item)
			if err != nil {
				return "", err
			}
			parts[i] = part
		}
		return "arr#" + rt.Raw.String() + "#" + strings.Join(parts, "|"), nil
	case TypeStruct:
		if !isMapKeyRuntimeType(rt) {
			return "", fmt.Errorf("%s is not comparable", rt.Raw)
		}
		st, _ := v.Ref.(*VMStruct)
		if st == nil {
			return "struct#" + rt.Raw.String() + "#nil", nil
		}
		parts := make([]string, 0, len(st.Fields))
		for i, field := range st.Fields {
			if field == nil {
				parts = append(parts, strconv.Itoa(i)+"=nil")
				continue
			}
			part, err := comparableMapKeyString(field.Value)
			if err != nil {
				return "", err
			}
			parts = append(parts, strconv.Itoa(i)+"="+part)
		}
		return "struct#" + rt.Raw.String() + "#" + strings.Join(parts, ";"), nil
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
