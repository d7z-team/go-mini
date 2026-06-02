package runtime

import (
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

const maxHostInt64 = uint64(1<<63 - 1)

func (e *Executor) ToVar(session *StackContext, val interface{}, bridge ffigo.FFIBridge) (*Var, error) {
	if val == nil {
		return nil, nil
	}

	res, err := e.hostValueToVar(session, val, bridge)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (e *Executor) hostValueToVar(session *StackContext, val interface{}, bridge ffigo.FFIBridge) (*Var, error) {
	switch v := val.(type) {
	case *Var:
		return v, nil
	case int:
		return NewInt(int64(v)), nil
	case int8:
		return NewInt(int64(v)), nil
	case int16:
		return NewInt(int64(v)), nil
	case int32:
		return NewInt(int64(v)), nil
	case int64:
		return NewInt(v), nil
	case uint:
		return hostUintToVar(uint64(v), val)
	case uint8:
		return NewInt(int64(v)), nil
	case uint16:
		return NewInt(int64(v)), nil
	case uint32:
		return NewInt(int64(v)), nil
	case uint64:
		return hostUintToVar(v, val)
	case float64:
		return NewFloat(v), nil
	case float32:
		return NewFloat(float64(v)), nil
	case string:
		return NewString(v), nil
	case []byte:
		if v == nil {
			return nil, nil
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		return NewByteArray(buf), nil
	case bool:
		return NewBool(v), nil
	case ffigo.InterfaceData:
		methods := make([]RuntimeInterfaceMethod, 0, len(v.Methods))
		for k, sig := range v.Methods {
			if strings.ContainsAny(k, "{};() ") {
				continue
			}
			methodSig, err := ParseRuntimeFuncSig(sig)
			if err != nil {
				continue
			}
			if err := CheckPublicRuntimeFuncSig("ffi interface method "+k, methodSig); err != nil {
				continue
			}
			methods = append(methods, RuntimeInterfaceMethod{Name: k, Spec: methodSig})
		}
		ifaceSpec, _ := ParseRuntimeInterfaceSpec(InterfaceType(methods))

		target := &Var{VType: TypeHostRef, Handle: v.Handle, Bridge: bridge}
		if v.Handle != 0 {
			target.Ref = NewVMHandle(v.Handle, bridge)
		}
		return &Var{
			VType: TypeInterface,
			Ref: &VMInterface{
				Target: target,
				Spec:   ifaceSpec,
			},
			Bridge: bridge,
		}, nil
	case ffigo.ErrorData:
		return newHostErrorVar(v, bridge), nil
	case map[string]interface{}:
		vmMap := &VMMap{Data: make(map[string]*Var), KeyVars: make(map[string]*Var)}
		for k, raw := range v {
			inner, err := e.ToVar(session, raw, bridge)
			if err != nil {
				return nil, fmt.Errorf("map value %q: %w", k, err)
			}
			vmMap.StoreWithKey(k, NewString(k), e.wrapAnyVar(inner))
		}
		res := &Var{VType: TypeMap, Ref: vmMap}
		res.SetRawType(MapType(SpecString, SpecAny).String())
		return res, nil
	case []interface{}:
		resArr := make([]*Var, len(v))
		for i, raw := range v {
			inner, err := e.ToVar(session, raw, bridge)
			if err != nil {
				return nil, fmt.Errorf("array item %d: %w", i, err)
			}
			resArr[i] = e.wrapAnyVar(inner)
		}
		res := &Var{VType: TypeArray, Ref: &VMArray{Data: resArr}}
		res.SetRawType(ArrayType(SpecAny).String())
		return res, nil
	default:
		return nil, fmt.Errorf("unsupported host value %T", v)
	}
}

func hostUintToVar(raw uint64, val interface{}) (*Var, error) {
	if raw > maxHostInt64 {
		return nil, fmt.Errorf("host value %T overflows Int64: %d", val, raw)
	}
	return NewInt(int64(raw)), nil
}

func (e *Executor) wrapAnyVar(inner *Var) *Var {
	if inner == nil {
		res := NewVarWithRuntimeType(MustParseRuntimeType(SpecAny), TypeAny)
		return res
	}
	if inner.VType == TypeAny {
		return inner
	}
	res := &Var{
		VType: TypeAny,
		Ref:   inner,
	}
	res.SetRawType(SpecAny.String())
	return res
}
