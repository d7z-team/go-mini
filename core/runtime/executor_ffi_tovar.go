package runtime

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"weak"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/typespec"
)

func (e *Executor) ToVar(session *StackContext, val interface{}, bridge ffigo.FFIBridge) (*Var, error) {
	if val == nil {
		return nil, nil
	}

	norm, err := e.normalizeValue(val)
	if err != nil {
		return nil, err
	}
	if norm == nil {
		return nil, nil
	}

	res, err := e.hostValueToVar(session, norm, bridge)
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
	case int64:
		return NewInt(v), nil
	case float64:
		return NewFloat(v), nil
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

func normalizeHostUint(raw uint64, typ interface{}) (int64, error) {
	if raw > math.MaxInt64 {
		return 0, fmt.Errorf("host value %T overflows Int64: %d", typ, raw)
	}
	return int64(raw), nil
}

func (e *Executor) normalizeValue(val interface{}) (interface{}, error) {
	if val == nil {
		return nil, nil
	}
	if _, ok := val.(*Var); ok {
		return val, nil
	}
	if _, ok := val.(ffigo.InterfaceData); ok {
		return val, nil
	}
	if _, ok := val.(ffigo.ErrorData); ok {
		return val, nil
	}
	if _, ok := val.(*ffigo.VMStruct); ok {
		return val, nil
	}

	v := reflect.ValueOf(val)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return normalizeHostUint(v.Uint(), val)
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if v.Kind() == reflect.Slice {
				return v.Bytes(), nil
			}
			res := make([]byte, v.Len())
			for i := 0; i < v.Len(); i++ {
				res[i] = uint8(v.Index(i).Uint())
			}
			return res, nil
		}
		res := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := e.normalizeValue(v.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			res[i] = item
		}
		return res, nil
	case reflect.Map:
		res := make(map[string]interface{}, v.Len())
		for _, key := range v.MapKeys() {
			if key.Kind() != reflect.String {
				return nil, fmt.Errorf("unsupported non-string map key kind %v", key.Kind())
			}
			item, err := e.normalizeValue(v.MapIndex(key).Interface())
			if err != nil {
				return nil, err
			}
			res[key.String()] = item
		}
		return res, nil
	case reflect.Struct:
		res := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
				name = strings.Split(tag, ",")[0]
			}
			item, err := e.normalizeValue(v.Field(i).Interface())
			if err != nil {
				return nil, err
			}
			res[name] = item
		}
		return res, nil
	default:
		return nil, fmt.Errorf("unsupported host value %T", val)
	}
}
