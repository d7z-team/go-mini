package runtime

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"strconv"
)

type FFIConstValue struct {
	Type    TypeSpec `json:"type"`
	Int64   *int64   `json:"int64,omitempty"`
	Float64 *float64 `json:"float64,omitempty"`
	String  *string  `json:"string,omitempty"`
	Bool    *bool    `json:"bool,omitempty"`
}

func ConstInt64(v int64) FFIConstValue {
	return FFIConstValue{Type: SpecInt64, Int64: &v}
}

func ConstFloat64(v float64) FFIConstValue {
	return FFIConstValue{Type: SpecFloat64, Float64: &v}
}

func ConstString(v string) FFIConstValue {
	return FFIConstValue{Type: SpecString, String: &v}
}

func ConstBool(v bool) FFIConstValue {
	return FFIConstValue{Type: SpecBool, Bool: &v}
}

func ConstantValue(v interface{}) (FFIConstValue, error) {
	if v == nil {
		return FFIConstValue{}, fmt.Errorf("unsupported ffi const type %T", v)
	}
	toInt64 := func(raw uint64, typ interface{}) (FFIConstValue, error) {
		if raw > math.MaxInt64 {
			return FFIConstValue{}, fmt.Errorf("ffi const %T overflows Int64: %d", typ, raw)
		}
		return ConstInt64(int64(raw)), nil
	}
	switch val := v.(type) {
	case bool:
		return ConstBool(val), nil
	case string:
		return ConstString(val), nil
	case float32:
		return ConstFloat64(float64(val)), nil
	case float64:
		return ConstFloat64(val), nil
	case int:
		return ConstInt64(int64(val)), nil
	case int8:
		return ConstInt64(int64(val)), nil
	case int16:
		return ConstInt64(int64(val)), nil
	case int32:
		return ConstInt64(int64(val)), nil
	case int64:
		return ConstInt64(val), nil
	case uint:
		return toInt64(uint64(val), v)
	case uint8:
		return ConstInt64(int64(val)), nil
	case uint16:
		return ConstInt64(int64(val)), nil
	case uint32:
		return ConstInt64(int64(val)), nil
	case uint64:
		return toInt64(val, v)
	case uintptr:
		return toInt64(uint64(val), v)
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return ConstBool(rv.Bool()), nil
	case reflect.String:
		return ConstString(rv.String()), nil
	case reflect.Float32, reflect.Float64:
		return ConstFloat64(rv.Convert(reflect.TypeOf(float64(0))).Float()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return ConstInt64(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return toInt64(rv.Uint(), v)
	default:
		return FFIConstValue{}, fmt.Errorf("unsupported ffi const type %T", v)
	}
}

func MustConstantValue(v interface{}) FFIConstValue {
	res, err := ConstantValue(v)
	if err != nil {
		panic(err)
	}
	return res
}

func (v FFIConstValue) Validate() error {
	typ, err := ParseRuntimeType(v.Type)
	if err != nil || typ.IsEmpty() {
		return fmt.Errorf("invalid ffi const type %s", v.Type)
	}
	count := 0
	if v.Int64 != nil {
		count++
	}
	if v.Float64 != nil {
		count++
	}
	if v.String != nil {
		count++
	}
	if v.Bool != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("ffi const %s must carry exactly one value payload", v.Type)
	}
	switch v.Type {
	case SpecInt64:
		if v.Int64 == nil {
			return fmt.Errorf("ffi const %s requires Int64 payload", v.Type)
		}
	case SpecFloat64:
		if v.Float64 == nil {
			return fmt.Errorf("ffi const %s requires Float64 payload", v.Type)
		}
	case SpecString:
		if v.String == nil {
			return fmt.Errorf("ffi const %s requires String payload", v.Type)
		}
	case SpecBool:
		if v.Bool == nil {
			return fmt.Errorf("ffi const %s requires Bool payload", v.Type)
		}
	default:
		return fmt.Errorf("unsupported ffi const type %s", v.Type)
	}
	return nil
}

func (v FFIConstValue) ToVar() *Var {
	switch v.Type {
	case SpecInt64:
		if v.Int64 != nil {
			return NewInt(*v.Int64)
		}
	case SpecFloat64:
		if v.Float64 != nil {
			return NewFloat(*v.Float64)
		}
	case SpecString:
		if v.String != nil {
			return NewString(*v.String)
		}
	case SpecBool:
		if v.Bool != nil {
			return NewBool(*v.Bool)
		}
	}
	return nil
}

func (v FFIConstValue) DisplayString() string {
	switch v.Type {
	case SpecInt64:
		if v.Int64 != nil {
			return strconv.FormatInt(*v.Int64, 10)
		}
	case SpecFloat64:
		if v.Float64 != nil {
			return strconv.FormatFloat(*v.Float64, 'g', -1, 64)
		}
	case SpecString:
		if v.String != nil {
			return *v.String
		}
	case SpecBool:
		if v.Bool != nil {
			return strconv.FormatBool(*v.Bool)
		}
	}
	return ""
}

func (v FFIConstValue) Hash() string {
	switch v.Type {
	case SpecInt64:
		if v.Int64 != nil {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], uint64(*v.Int64))
			return VersionedExternalRequirementHash("const-value", v.Type.String(), hex.EncodeToString(buf[:]))
		}
	case SpecFloat64:
		if v.Float64 != nil {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], math.Float64bits(*v.Float64))
			return VersionedExternalRequirementHash("const-value", v.Type.String(), hex.EncodeToString(buf[:]))
		}
	case SpecString:
		if v.String != nil {
			return VersionedExternalRequirementHash("const-value", v.Type.String(), *v.String)
		}
	case SpecBool:
		if v.Bool != nil {
			if *v.Bool {
				return VersionedExternalRequirementHash("const-value", v.Type.String(), "1")
			}
			return VersionedExternalRequirementHash("const-value", v.Type.String(), "0")
		}
	}
	return VersionedExternalRequirementHash("const-value", v.Type.String(), "")
}

func cloneFFIConstValueMap(in map[string]FFIConstValue) map[string]FFIConstValue {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]FFIConstValue, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
