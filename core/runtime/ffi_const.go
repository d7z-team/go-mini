package runtime

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
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
