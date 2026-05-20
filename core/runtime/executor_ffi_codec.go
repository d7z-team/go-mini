package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"weak"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) serializeRuntimeType(buf *ffigo.Buffer, v *Var, typ RuntimeType) error {
	return e.serializeParsedType(buf, v, typ)
}

func (e *Executor) deserializeRuntimeType(session *StackContext, reader *ffigo.Reader, typ RuntimeType, bridge ffigo.FFIBridge) (*Var, error) {
	res, err := e.deserializeParsedType(session, reader, typ, bridge)
	if res != nil {
		res.SetRuntimeType(typ)
		if session != nil && session.Stack != nil {
			res.stack = weak.Make(session.Stack)
		}
	}
	return res, err
}

func (e *Executor) serializeStructSchema(buf *ffigo.Buffer, v *Var, schema *RuntimeStructSpec) error {
	if schema == nil {
		return nil
	}
	var st *VMStruct
	if v != nil && v.VType == TypeStruct {
		st, _ = v.Ref.(*VMStruct)
	}
	if v != nil && st == nil {
		return fmt.Errorf("expected struct %s, got %v", schema.TypeInfo.Raw, v.VType)
	}
	for _, field := range schema.Fields {
		var fVal *Var
		if st != nil {
			if slot, ok := st.Field(field.Name); ok && slot != nil {
				fVal = slot.Value
			}
		}
		if err := e.serializeRuntimeType(buf, fVal, field.TypeInfo); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) deserializeStructSchema(session *StackContext, reader *ffigo.Reader, schema *RuntimeStructSpec, bridge ffigo.FFIBridge) (*Var, error) {
	if schema == nil {
		return nil, nil
	}
	fields := make([]*Slot, len(schema.Fields))
	byName := make(map[string]int, len(schema.Fields))
	for i, field := range schema.Fields {
		val, err := e.deserializeRuntimeType(session, reader, field.TypeInfo, bridge)
		if err != nil {
			return nil, err
		}
		fields[i] = NewSlot(field.TypeInfo, val)
		byName[field.Name] = i
	}
	v := &Var{VType: TypeStruct, Ref: &VMStruct{Spec: schema, Fields: fields, ByName: byName}}
	v.SetRuntimeType(schema.TypeInfo)
	return v, nil
}

func (e *Executor) serializeKey(buf *ffigo.Buffer, key string, kType RuntimeType) error {
	switch kType.Raw {
	case "String":
		buf.WriteString(key)
	case "Int64", "Int", "int", "int64":
		key = strings.TrimPrefix(key, "i:")
		v, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse map key '%s' as Int64: %w", key, err)
		}
		buf.WriteVarint(v)
	case "Bool", "bool":
		key = strings.TrimPrefix(key, "b:")
		v, err := strconv.ParseBool(key)
		if err != nil {
			return fmt.Errorf("failed to parse map key '%s' as Bool: %w", key, err)
		}
		buf.WriteBool(v)
	case "Float64", "float64":
		key = strings.TrimPrefix(key, "f:")
		v, err := strconv.ParseFloat(key, 64)
		if err != nil {
			return fmt.Errorf("failed to parse map key '%s' as Float64: %w", key, err)
		}
		buf.WriteFloat64(v)
	default:
		return fmt.Errorf("unsupported map key type: %s", kType.Raw)
	}
	return nil
}

func (e *Executor) lookupStructSchema(typ RuntimeType) (*RuntimeStructSpec, bool) {
	if typ.Raw != "" {
		if schema, ok := e.resolveStructSchema(typ.Raw); ok {
			return schema, true
		}
	}
	if typ.TypeID == "" {
		return nil, false
	}
	return e.resolveStructSchema(TypeSpec(typ.TypeID))
}

func (e *Executor) unwrapFFIValue(v *Var) *Var {
	return e.unwrapValue(v)
}

func (e *Executor) serializeParsedType(buf *ffigo.Buffer, v *Var, typ RuntimeType) error {
	v = e.unwrapFFIValue(v)

	switch typ.Kind {
	case RuntimeTypeVoid:
		return nil
	case RuntimeTypeAny:
		e.serializeVarToAny(buf, v)
		return nil
	case RuntimeTypePrimitive, RuntimeTypeNamed:
		switch typ.Raw {
		case "String":
			str := ""
			if v != nil {
				str = v.Str
				if v.VType == TypeBytes {
					str = string(v.B)
				}
				if v.VType == TypeError {
					str, _ = v.ToError()
				}
			}
			buf.WriteString(str)
			return nil
		case "Float64":
			fVal := 0.0
			if v != nil {
				fVal, _ = v.ToFloat()
			}
			buf.WriteFloat64(fVal)
			return nil
		case "Uint32", "uint32", "Int32", "int32":
			iVal := int64(0)
			if v != nil {
				iVal, _ = v.ToInt()
			}
			buf.WriteUvarint(uint64(iVal))
			return nil
		case "Bool":
			bVal := false
			if v != nil {
				bVal, _ = v.ToBool()
			}
			buf.WriteBool(bVal)
			return nil
		case "Error", "error":
			msg := ""
			handle := uint32(0)
			if v != nil {
				msg, _ = v.ToError()
				if v.VType == TypeError {
					if err, ok := v.Ref.(*VMError); ok {
						handle = err.Handle
					}
				}
			}
			buf.WriteRawError(msg, handle)
			return nil
		case "TypeBytes":
			var bVal []byte
			if v != nil {
				bVal, _ = v.ToBytes()
			}
			buf.WriteBytes(bVal)
			return nil
		}
		if typ.Raw.IsNumeric() {
			iVal := int64(0)
			if v != nil {
				iVal, _ = v.ToInt()
			}
			buf.WriteVarint(iVal)
			return nil
		}
		if schema, ok := e.lookupStructSchema(typ); ok {
			if schema.Ownership == StructOwnershipHostOpaque {
				return fmt.Errorf("cannot serialize bare host opaque value %s; use HostRef<%s>", typ.Raw, schema.TypeID)
			}
			return e.serializeStructSchema(buf, v, schema)
		}
		e.serializeVarToAny(buf, v)
		return nil
	case RuntimeTypeHostRef:
		if v == nil {
			buf.WriteUvarint(0)
			return nil
		}
		if !e.isOpaqueHandle(v) {
			return fmt.Errorf("cannot pass %v as %s: expected opaque host reference", v.VType, typ.Raw)
		}
		buf.WriteUvarint(uint64(v.Handle))
		return nil
	case RuntimeTypePointer:
		if _, ok := e.vmPointerTarget(v); !ok && v != nil {
			return fmt.Errorf("cannot pass %v as VM pointer %s", v.VType, typ.Raw)
		}
		e.serializeVarToAny(buf, v)
		return nil
	case RuntimeTypeArray:
		if v == nil || v.VType != TypeArray {
			buf.WriteUvarint(0)
			return nil
		}
		arr := v.Ref.(*VMArray)
		items := arr.Snapshot()
		buf.WriteUvarint(uint64(len(items)))
		for _, item := range items {
			if err := e.serializeParsedType(buf, item, *typ.Elem); err != nil {
				return err
			}
		}
		return nil
	case RuntimeTypeMap:
		if v == nil || v.VType != TypeMap {
			buf.WriteUvarint(0)
			return nil
		}
		vmMap := v.Ref.(*VMMap)
		snapshot := vmMap.Snapshot()
		buf.WriteUvarint(uint64(len(snapshot)))
		for k, val := range snapshot {
			if err := e.serializeKey(buf, k, *typ.Key); err != nil {
				return err
			}
			if err := e.serializeParsedType(buf, val, *typ.Value); err != nil {
				return err
			}
		}
		return nil
	case RuntimeTypeTuple:
		if v == nil || v.VType != TypeArray {
			for range typ.Params {
				buf.WriteAny(nil)
			}
			return nil
		}
		arr := v.Ref.(*VMArray)
		items := arr.Snapshot()
		for i, t := range typ.Params {
			var arg *Var
			if i < len(items) {
				arg = items[i]
			}
			if err := e.serializeParsedType(buf, arg, t); err != nil {
				return err
			}
		}
		return nil
	case RuntimeTypeInterface:
		if v == nil || v.VType != TypeInterface || v.Ref == nil {
			buf.WriteRawInterface(0, nil)
			return nil
		}
		if iface, ok := v.Ref.(*VMInterface); ok {
			buf.WriteRawInterface(iface.Target.Handle, iface.Spec.MethodStringMap())
		} else {
			buf.WriteRawInterface(0, nil)
		}
		return nil
	case RuntimeTypeStruct:
		return e.serializeStructSchema(buf, v, &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: typ.Fields})
	case RuntimeTypeFunction:
		e.serializeVarToAny(buf, v)
		return nil
	default:
		e.serializeVarToAny(buf, v)
		return nil
	}
}

func (e *Executor) serializeVarToAny(buf *ffigo.Buffer, v *Var) {
	v = e.unwrapFFIValue(v)
	if v == nil {
		buf.WriteAny(nil)
		return
	}
	switch v.VType {
	case TypeInt:
		buf.WriteAny(v.I64)
	case TypeFloat:
		buf.WriteAny(v.F64)
	case TypeString:
		buf.WriteAny(v.Str)
	case TypeBytes:
		buf.WriteAny(v.B)
	case TypeBool:
		buf.WriteAny(v.Bool)
	case TypeError:
		if err, ok := v.Ref.(*VMError); ok {
			_ = buf.WriteByte(ffigo.TypeTagError)
			buf.WriteRawError(err.Message, err.Handle)
		} else {
			buf.WriteAny(nil)
		}
	case TypeHandle:
		// Internal VM pointers travel as pointer-tagged Any values.
		if inner, ok := e.vmPointerTarget(v); ok {
			_ = buf.WriteByte(ffigo.TypeTagPointer)
			e.serializeVarToAny(buf, inner)
			return
		}
		buf.WriteAny(nil)
	case TypeHostRef:
		buf.WriteAny(v.Handle)
	case TypeArray:
		arr := v.Ref.(*VMArray)
		items := arr.Snapshot()
		_ = buf.WriteByte(ffigo.TypeTagArray)
		buf.WriteUvarint(uint64(len(items)))
		for _, item := range items {
			e.serializeVarToAny(buf, item)
		}
	case TypeMap:
		vmMap := v.Ref.(*VMMap)
		_ = buf.WriteByte(ffigo.TypeTagMap)
		snapshot := vmMap.Snapshot()
		buf.WriteUvarint(uint64(len(snapshot)))
		for k, val := range snapshot {
			buf.WriteString(k)
			e.serializeVarToAny(buf, val)
		}
	case TypeStruct:
		st := v.Ref.(*VMStruct)
		_ = buf.WriteByte(ffigo.TypeTagStruct)
		if st.Spec == nil {
			buf.WriteUvarint(0)
			return
		}
		buf.WriteUvarint(uint64(len(st.Spec.Fields)))
		for i, field := range st.Spec.Fields {
			buf.WriteString(field.Name)
			var fieldVal *Var
			if i < len(st.Fields) && st.Fields[i] != nil {
				fieldVal = st.Fields[i].Value
			}
			e.serializeVarToAny(buf, fieldVal)
		}
	case TypeInterface:
		if v.Ref == nil {
			_ = buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(0, nil)
			return
		}
		if iface, ok := v.Ref.(*VMInterface); ok {
			_ = buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(iface.Target.Handle, iface.Spec.MethodStringMap())
		} else {
			_ = buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(0, nil)
		}
	default:
		buf.WriteAny(nil)
	}
}

func (e *Executor) deserializeKey(reader *ffigo.Reader, kType RuntimeType) (string, error) {
	switch kType.Raw {
	case "String":
		return reader.ReadString(), nil
	case "Int64", "Int", "int", "int64":
		return "i:" + strconv.FormatInt(reader.ReadVarint(), 10), nil
	case "Bool", "bool":
		return "b:" + strconv.FormatBool(reader.ReadBool()), nil
	case "Float64", "float64":
		return "f:" + strconv.FormatFloat(reader.ReadFloat64(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported map key type: %s", kType.Raw)
	}
}

func (e *Executor) deserializeParsedType(session *StackContext, reader *ffigo.Reader, typ RuntimeType, bridge ffigo.FFIBridge) (*Var, error) {
	if typ.Kind == RuntimeTypeVoid {
		return nil, nil
	}
	if reader.Available() == 0 {
		return nil, nil
	}

	var (
		res *Var
		err error
	)

	switch typ.Kind {
	case RuntimeTypeAny:
		res = e.wrapAnyVar(session, e.ToVar(session, reader.ReadAny(), bridge))
	case RuntimeTypePrimitive, RuntimeTypeNamed:
		switch typ.Raw {
		case "String":
			res = NewString(reader.ReadString())
		case "Int64", "int", "int64":
			res = NewInt(reader.ReadVarint())
		case "Uint32", "uint32", "Int32", "int32":
			res = NewInt(int64(reader.ReadUvarint()))
		case "Float64":
			res = NewFloat(reader.ReadFloat64())
		case "Bool":
			res = NewBool(reader.ReadBool())
		case "Error", "error":
			res = e.ToVar(session, reader.ReadRawError(), bridge)
		case "TypeBytes":
			res = &Var{VType: TypeBytes, B: reader.ReadBytes()}
		default:
			if typ.Raw.IsNumeric() {
				res = NewInt(reader.ReadVarint())
				break
			}
			if schema, ok := e.lookupStructSchema(typ); ok {
				if schema.Ownership == StructOwnershipHostOpaque {
					err = fmt.Errorf("cannot deserialize bare host opaque value %s; use HostRef<%s>", typ.Raw, schema.TypeID)
					break
				}
				res, err = e.deserializeStructSchema(session, reader, schema, bridge)
				break
			}
			err = fmt.Errorf("unsupported named FFI return type: %s", typ.Raw)
		}
	case RuntimeTypeHostRef:
		id := uint32(reader.ReadUvarint())
		var h *VMHandle
		if id != 0 {
			h = NewVMHandle(id, bridge)
		}
		res = &Var{VType: TypeHostRef, Handle: id, Bridge: bridge, Ref: h}
	case RuntimeTypePointer:
		err = fmt.Errorf("FFI cannot return VM pointer type %s", typ.Raw)
	case RuntimeTypeArray:
		count := int(reader.ReadUvarint())
		arrData := make([]*Var, count)
		for i := 0; i < count; i++ {
			val, err := e.deserializeParsedType(session, reader, *typ.Elem, bridge)
			if err != nil {
				return nil, err
			}
			arrData[i] = val
		}
		res = &Var{VType: TypeArray, Ref: &VMArray{Data: arrData}}
	case RuntimeTypeMap:
		count := int(reader.ReadUvarint())
		mapData := make(map[string]*Var, count)
		for i := 0; i < count; i++ {
			k, err := e.deserializeKey(reader, *typ.Key)
			if err != nil {
				return nil, err
			}
			val, err := e.deserializeParsedType(session, reader, *typ.Value, bridge)
			if err != nil {
				return nil, err
			}
			mapData[k] = val
		}
		res = &Var{VType: TypeMap, Ref: &VMMap{Data: mapData}}
	case RuntimeTypeTuple:
		tupleData := make([]*Var, len(typ.Params))
		for i, t := range typ.Params {
			val, err := e.deserializeParsedType(session, reader, t, bridge)
			if err != nil {
				return nil, err
			}
			tupleData[i] = val
		}
		res = &Var{VType: TypeArray, Ref: &VMArray{Data: tupleData}}
	case RuntimeTypeInterface:
		res = e.ToVar(session, reader.ReadRawInterface(), bridge)
	case RuntimeTypeStruct:
		res, err = e.deserializeStructSchema(session, reader, &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: typ.Fields}, bridge)
	case RuntimeTypeFunction:
		res = e.wrapAnyVar(session, e.ToVar(session, reader.ReadAny(), bridge))
	default:
		return nil, fmt.Errorf("unsupported FFI return type: %s", typ.Raw)
	}

	if err != nil {
		return nil, err
	}
	if res != nil {
		res.SetRuntimeType(typ)
		if session != nil && session.Stack != nil {
			res.stack = weak.Make(session.Stack)
		}
	}
	return res, nil
}
