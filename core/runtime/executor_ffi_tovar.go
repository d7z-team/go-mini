package runtime

import (
	"fmt"
	"strings"
	"weak"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/typespec"
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
	if res != nil && session != nil && session.Stack != nil {
		res.stack = weak.Make(session.Stack)
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
		return NewBytes(buf), nil
	case bool:
		return NewBool(v), nil
	case uint32:
		var h *VMHandle
		if v != 0 {
			h = NewVMHandle(v, bridge)
		}
		res := &Var{VType: TypeHostRef, Handle: v, Bridge: bridge, Ref: h}
		res.SetRawType(HostRefType(SpecAny).String())
		return res, nil
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
		target.SetRawType(HostRefType(SpecAny).String())
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
			vmMap.StoreWithKey(k, NewString(k), e.wrapAnyVar(session, inner))
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
			resArr[i] = e.wrapAnyVar(session, inner)
		}
		res := &Var{VType: TypeArray, Ref: &VMArray{Data: resArr}}
		res.SetRawType(ArrayType(SpecAny).String())
		return res, nil
	case *ffigo.VMStruct:
		if e.metadata != nil && v.TypeName != "" {
			if schema, ok := e.resolveStructSchema(TypeSpec(v.TypeName)); ok && schema != nil {
				if schema.Ownership != StructOwnershipVMValue {
					return nil, fmt.Errorf("FFI struct %s is not VMValue", v.TypeName)
				}
				return e.decodeKnownVMStruct(session, v, bridge, schema)
			}
		}
		return e.decodeAnonymousVMStruct(session, v, bridge)
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

func (e *Executor) decodeKnownVMStruct(session *StackContext, raw *ffigo.VMStruct, bridge ffigo.FFIBridge, schema *RuntimeStructSpec) (*Var, error) {
	fields := make([]*Slot, len(schema.Fields))
	byName := make(map[string]int, len(schema.Fields))
	for i, field := range schema.Fields {
		var fieldVal *Var
		for _, rawField := range raw.Fields {
			if rawField.Name != field.Name {
				continue
			}
			decoded, err := e.ToVar(session, rawField.Value, bridge)
			if err != nil {
				return nil, fmt.Errorf("struct %s field %s: %w", schema.TypeID, field.Name, err)
			}
			fieldVal = decoded
			break
		}
		if fieldVal != nil {
			prepared, err := e.prepareValueForType(session, fieldVal, field.TypeInfo)
			if err != nil {
				return nil, fmt.Errorf("struct %s field %s: %w", schema.TypeID, field.Name, err)
			}
			fieldVal = prepared
		}
		fields[i] = NewSlot(field.TypeInfo, fieldVal)
		byName[field.Name] = i
	}
	res := &Var{VType: TypeStruct, Ref: &VMStruct{Spec: schema, Fields: fields, ByName: byName}}
	res.SetRuntimeType(schema.TypeInfo)
	return res, nil
}

func (e *Executor) decodeAnonymousVMStruct(session *StackContext, raw *ffigo.VMStruct, bridge ffigo.FFIBridge) (*Var, error) {
	fields := make([]*Slot, len(raw.Fields))
	byName := make(map[string]int, len(raw.Fields))
	specFields := make([]RuntimeStructField, len(raw.Fields))
	for i, field := range raw.Fields {
		val, err := e.ToVar(session, field.Value, bridge)
		if err != nil {
			return nil, fmt.Errorf("struct field %s: %w", field.Name, err)
		}
		fieldType := MustParseRuntimeType("Any")
		if val != nil && !val.RuntimeType().IsEmpty() {
			fieldType = val.RuntimeType()
		}
		fields[i] = NewSlot(fieldType, val)
		byName[field.Name] = i
		specFields[i] = RuntimeStructField{Name: field.Name, TypeInfo: fieldType}
	}
	members := make([]typespec.Member, 0, len(specFields))
	for _, field := range specFields {
		members = append(members, typespec.Member{Name: field.Name, Type: field.TypeInfo.Raw})
	}
	specType := typespec.Struct(members)
	spec := &RuntimeStructSpec{Spec: specType, TypeInfo: MustParseRuntimeType(specType), Fields: specFields}
	res := &Var{VType: TypeStruct, Ref: &VMStruct{Spec: spec, Fields: fields, ByName: byName}}
	res.SetRuntimeType(spec.TypeInfo)
	return res, nil
}

func (e *Executor) wrapAnyVar(session *StackContext, inner *Var) *Var {
	if inner == nil {
		res := NewVarWithRuntimeType(MustParseRuntimeType(SpecAny), TypeAny)
		if session != nil && session.Stack != nil {
			res.stack = weak.Make(session.Stack)
		}
		return res
	}
	if inner.VType == TypeAny {
		return inner
	}
	res := &Var{
		VType:  TypeAny,
		Ref:    inner,
		Bridge: inner.Bridge,
		Handle: inner.Handle,
	}
	res.SetRawType(SpecAny.String())
	if session != nil && session.Stack != nil {
		res.stack = weak.Make(session.Stack)
	}
	return res
}
