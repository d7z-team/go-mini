package runtime

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) serializeRuntimeType(buf *ffigo.Buffer, v *Var, typ RuntimeType) error {
	return e.serializeParsedType(buf, v, typ)
}

func (e *Executor) deserializeRuntimeType(session *StackContext, reader *ffigo.Reader, typ RuntimeType, bridge ffigo.FFIBridge) (*Var, error) {
	res, err := e.deserializeParsedType(session, reader, typ, bridge)
	if res != nil {
		res.SetRuntimeType(typ)
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

func (e *Executor) serializeKey(buf *ffigo.Buffer, key *Var, kType RuntimeType) error {
	key = e.unwrapFFIValue(key)
	switch kType.Raw {
	case "String":
		if key == nil {
			buf.WriteString("")
			return nil
		}
		if key.VType != TypeString {
			return fmt.Errorf("FFI encode map key String: expected String, got %v", key.VType)
		}
		buf.WriteString(key.Str)
	case "Int64", "Int", "int", "int64":
		if key == nil {
			buf.WriteVarint(0)
			return nil
		}
		v, err := key.ToInt()
		if err != nil {
			return fmt.Errorf("FFI encode map key Int64: %w", err)
		}
		buf.WriteVarint(v)
	case "Bool", "bool":
		if key == nil {
			buf.WriteBool(false)
			return nil
		}
		v, err := key.ToBool()
		if err != nil {
			return fmt.Errorf("FFI encode map key Bool: %w", err)
		}
		buf.WriteBool(v)
	case "Float64", "float64":
		if key == nil {
			buf.WriteFloat64(0)
			return nil
		}
		v, err := key.ToFloat()
		if err != nil {
			return fmt.Errorf("FFI encode map key Float64: %w", err)
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
		return e.serializeVarToAny(buf, v)
	case RuntimeTypePrimitive, RuntimeTypeNamed:
		switch typ.Raw {
		case "String":
			if v == nil {
				buf.WriteString("")
				return nil
			}
			switch v.VType {
			case TypeString:
				buf.WriteString(v.Str)
				return nil
			case TypeBytes:
				buf.WriteString(string(v.B))
				return nil
			case TypeError:
				str, err := v.ToError()
				if err != nil {
					return err
				}
				buf.WriteString(str)
				return nil
			default:
				return fmt.Errorf("FFI encode String: expected String, got %v", v.VType)
			}
		case "Float64":
			fVal := 0.0
			if v != nil {
				var err error
				fVal, err = v.ToFloat()
				if err != nil {
					return err
				}
			}
			buf.WriteFloat64(fVal)
			return nil
		case "Uint32", "uint32", "Int32", "int32":
			iVal := int64(0)
			if v != nil {
				var err error
				iVal, err = v.ToInt()
				if err != nil {
					return err
				}
			}
			buf.WriteUvarint(uint64(iVal))
			return nil
		case "Bool":
			bVal := false
			if v != nil {
				var err error
				bVal, err = v.ToBool()
				if err != nil {
					return err
				}
			}
			buf.WriteBool(bVal)
			return nil
		case "Error", "error":
			msg := ""
			handle := uint32(0)
			if v != nil {
				msg, _ = v.ToError()
				if host := hostErrorFromError(goErrorFromVar(v)); host != nil {
					handle = host.Handle
				}
			}
			buf.WriteRawError(msg, handle)
			return nil
		case "TypeBytes":
			var bVal []byte
			if v != nil {
				var err error
				bVal, err = v.ToBytes()
				if err != nil {
					return err
				}
			}
			buf.WriteBytes(bVal)
			return nil
		}
		if typ.Raw.IsNumeric() {
			iVal := int64(0)
			if v != nil {
				var err error
				iVal, err = v.ToInt()
				if err != nil {
					return err
				}
			}
			buf.WriteVarint(iVal)
			return nil
		}
		if typ.Kind == RuntimeTypeNamed {
			if spec, ok := e.resolveInterfaceSpec(typ.Raw); ok {
				return e.serializeInterfaceValue(buf, v, typ.Raw, spec)
			}
		}
		if schema, ok := e.lookupStructSchema(typ); ok {
			if schema.Ownership == StructOwnershipHostOpaque {
				return fmt.Errorf("cannot serialize bare host opaque value %s; use HostRef<%s>", typ.Raw, schema.TypeID)
			}
			return e.serializeStructSchema(buf, v, schema)
		}
		return e.serializeVarToAny(buf, v)
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
		return fmt.Errorf("FFI cannot accept VM pointer type %s", typ.Raw)
	case RuntimeTypeArray:
		if v == nil || v.VType != TypeArray {
			buf.WriteUvarint(0)
			return nil
		}
		arr := arrayRef(v)
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
		vmMap := mapRef(v)
		entries := vmMap.Entries()
		buf.WriteUvarint(uint64(len(entries)))
		for _, entry := range entries {
			keyVar := entry.Key
			if keyVar == nil && typ.Key != nil && typ.Key.IsString() {
				keyVar = NewString(entry.Encoded)
			}
			if err := e.serializeKey(buf, keyVar, *typ.Key); err != nil {
				return err
			}
			if err := e.serializeParsedType(buf, entry.Value, *typ.Value); err != nil {
				return err
			}
		}
		return nil
	case RuntimeTypeChannel:
		id := uint64(0)
		if ch, ok := asVMChannel(v); ok {
			id = e.registerVMChannelEndpoint(ch, typ)
		}
		buf.WriteUvarint(id)
		return nil
	case RuntimeTypeTuple:
		if v == nil || v.VType != TypeArray {
			for range typ.Params {
				buf.WriteAny(nil)
			}
			return nil
		}
		arr := arrayRef(v)
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
		return e.serializeInterfaceValue(buf, v, typ.Raw, nil)
	case RuntimeTypeStruct:
		return e.serializeStructSchema(buf, v, &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: typ.Fields})
	case RuntimeTypeFunction:
		return e.serializeVarToAny(buf, v)
	default:
		return e.serializeVarToAny(buf, v)
	}
}

func (e *Executor) serializeInterfaceValue(buf *ffigo.Buffer, v *Var, interfaceType TypeSpec, spec *RuntimeInterfaceSpec) error {
	v = e.unwrapFFIValue(v)
	if v == nil {
		buf.WriteRawInterface(0, nil)
		return nil
	}
	if v.VType != TypeInterface {
		checked, err := e.CheckSatisfaction(v, interfaceType.String())
		if err != nil {
			return err
		}
		v = e.unwrapFFIValue(checked)
	}
	if v == nil || v.VType != TypeInterface || v.Ref == nil {
		buf.WriteRawInterface(0, nil)
		return nil
	}
	iface, ok := v.Ref.(*VMInterface)
	if !ok {
		return fmt.Errorf("cannot serialize %v as FFI interface %s", v.VType, interfaceType)
	}
	if iface.Target == nil {
		buf.WriteRawInterface(0, nil)
		return nil
	}
	if !e.isOpaqueHandle(iface.Target) {
		return fmt.Errorf("cannot pass VM-only interface %s to FFI: target is %s, not host reference", interfaceType, iface.Target.RawType())
	}
	handle := iface.Target.Handle
	if handle == 0 {
		buf.WriteRawInterface(0, nil)
		return nil
	}
	if spec == nil {
		spec = iface.Spec
	}
	var methods map[string]string
	if spec != nil {
		methods = spec.MethodStringMap()
	}
	buf.WriteRawInterface(handle, methods)
	return nil
}

func (e *Executor) serializeVarToAny(buf *ffigo.Buffer, v *Var) error {
	if err := e.validateFFIAnyValue(v); err != nil {
		return err
	}
	v = e.unwrapFFIValue(v)
	if v == nil {
		buf.WriteAny(nil)
		return nil
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
		if err := goErrorFromVar(v); err != nil {
			handle := uint32(0)
			if host := hostErrorFromError(err); host != nil {
				handle = host.Handle
			}
			if handle != 0 {
				return errors.New("FFI Any cannot carry host error handle")
			}
			_ = buf.WriteByte(ffigo.TypeTagError)
			buf.WriteRawError(err.Error(), 0)
		} else {
			buf.WriteAny(nil)
		}
	case TypePointer:
		return errors.New("FFI Any cannot carry VM pointer")
	case TypeHostRef:
		return errors.New("FFI Any cannot carry host reference")
	case TypeChannel:
		return errors.New("FFI Any cannot carry channel")
	case TypeArray:
		arr := arrayRef(v)
		items := arr.Snapshot()
		_ = buf.WriteByte(ffigo.TypeTagArray)
		buf.WriteUvarint(uint64(len(items)))
		for _, item := range items {
			if err := e.serializeVarToAny(buf, item); err != nil {
				return err
			}
		}
	case TypeMap:
		vmMap := mapRef(v)
		_ = buf.WriteByte(ffigo.TypeTagMap)
		entries := vmMap.Entries()
		buf.WriteUvarint(uint64(len(entries)))
		for _, entry := range entries {
			key := entry.Key
			if key == nil {
				key = NewString(entry.Encoded)
			}
			rawKey, err := e.ffiMapStringKey(key)
			if err != nil {
				return err
			}
			buf.WriteString(rawKey)
			if err := e.serializeVarToAny(buf, entry.Value); err != nil {
				return err
			}
		}
	case TypeStruct:
		st := v.Ref.(*VMStruct)
		_ = buf.WriteByte(ffigo.TypeTagStruct)
		typeName := ""
		if st.Spec != nil {
			if st.Spec.TypeID != "" {
				typeName = st.Spec.TypeID
			} else if !st.Spec.TypeInfo.IsEmpty() {
				typeName = st.Spec.TypeInfo.Raw.String()
			}
		}
		buf.WriteString(typeName)
		if st.Spec == nil {
			buf.WriteUvarint(0)
			return nil
		}
		buf.WriteUvarint(uint64(len(st.Spec.Fields)))
		for i, field := range st.Spec.Fields {
			buf.WriteString(field.Name)
			var fieldVal *Var
			if i < len(st.Fields) && st.Fields[i] != nil {
				fieldVal = st.Fields[i].Value
			}
			if err := e.serializeVarToAny(buf, fieldVal); err != nil {
				return err
			}
		}
	case TypeInterface:
		return errors.New("FFI Any cannot carry interface")
	case TypeClosure:
		return errors.New("FFI Any cannot carry closure")
	default:
		buf.WriteAny(nil)
	}
	return nil
}

func (e *Executor) deserializeKey(reader *ffigo.Reader, kType RuntimeType) (*Var, error) {
	switch kType.Raw {
	case "String":
		v, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		return NewString(v), nil
	case "Int64", "Int", "int", "int64":
		v, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}
		return NewInt(v), nil
	case "Bool", "bool":
		v, err := reader.ReadBool()
		if err != nil {
			return nil, err
		}
		return NewBool(v), nil
	case "Float64", "float64":
		v, err := reader.ReadFloat64()
		if err != nil {
			return nil, err
		}
		return NewFloat(v), nil
	default:
		return nil, fmt.Errorf("unsupported map key type: %s", kType.Raw)
	}
}

func readRuntimeWireCount(reader *ffigo.Reader, label string) (int, error) {
	return reader.ReadCount(ffigo.MaxWireCollectionItems, label)
}

func (e *Executor) deserializeParsedType(session *StackContext, reader *ffigo.Reader, typ RuntimeType, bridge ffigo.FFIBridge) (*Var, error) {
	if typ.Kind == RuntimeTypeVoid {
		return nil, nil
	}
	if err := reader.Err(); err != nil {
		return nil, err
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
		raw, readErr := reader.ReadAny()
		if readErr != nil {
			err = readErr
			break
		}
		if err = rejectHostIdentityInAny(raw); err == nil {
			decoded, convErr := e.ToVar(session, raw, bridge)
			if convErr != nil {
				err = convErr
				break
			}
			res = e.wrapAnyVar(decoded)
		}
	case RuntimeTypePrimitive, RuntimeTypeNamed:
		switch typ.Raw {
		case "String":
			var v string
			v, err = reader.ReadString()
			res = NewString(v)
		case "Int64", "int", "int64":
			var v int64
			v, err = reader.ReadVarint()
			res = NewInt(v)
		case "Uint32", "uint32", "Int32", "int32":
			var v uint64
			v, err = reader.ReadUvarint()
			res = NewInt(int64(v))
		case "Float64":
			var v float64
			v, err = reader.ReadFloat64()
			res = NewFloat(v)
		case "Bool":
			var v bool
			v, err = reader.ReadBool()
			res = NewBool(v)
		case "Error", "error":
			var raw ffigo.ErrorData
			raw, err = reader.ReadRawError()
			if err == nil {
				res, err = e.ToVar(session, raw, bridge)
			}
		case "TypeBytes":
			var v []byte
			v, err = reader.ReadBytes()
			res = &Var{VType: TypeBytes, B: v}
		default:
			if typ.Raw.IsNumeric() {
				var v int64
				v, err = reader.ReadVarint()
				res = NewInt(v)
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
			if typ.Kind == RuntimeTypeNamed {
				if _, ok := e.resolveInterfaceSpec(typ.Raw); ok {
					var raw ffigo.InterfaceData
					raw, err = reader.ReadRawInterface()
					if err == nil {
						res, err = e.ToVar(session, raw, bridge)
					}
					break
				}
			}
			err = fmt.Errorf("unsupported named FFI return type: %s", typ.Raw)
		}
	case RuntimeTypeHostRef:
		rawID, readErr := reader.ReadUvarint()
		if readErr != nil {
			return nil, readErr
		}
		id := uint32(rawID)
		var h *VMHandle
		if id != 0 {
			h = NewVMHandle(id, bridge)
		}
		res = &Var{VType: TypeHostRef, Handle: id, Bridge: bridge, Ref: h}
	case RuntimeTypePointer:
		err = fmt.Errorf("FFI cannot return VM pointer type %s", typ.Raw)
	case RuntimeTypeArray:
		count, err := readRuntimeWireCount(reader, "array")
		if err != nil {
			return nil, err
		}
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
		count, err := readRuntimeWireCount(reader, "map")
		if err != nil {
			return nil, err
		}
		vmMap := &VMMap{Data: make(map[string]*Var, count), KeyVars: make(map[string]*Var, count)}
		for i := 0; i < count; i++ {
			keyVar, err := e.deserializeKey(reader, *typ.Key)
			if err != nil {
				return nil, err
			}
			if err := reader.Err(); err != nil {
				return nil, err
			}
			val, err := e.deserializeParsedType(session, reader, *typ.Value, bridge)
			if err != nil {
				return nil, err
			}
			encodedKey, storedKey, err := e.comparableMapKey(keyVar, *typ.Key)
			if err != nil {
				return nil, err
			}
			vmMap.StoreWithKey(encodedKey, storedKey, val)
		}
		res = &Var{VType: TypeMap, Ref: vmMap}
	case RuntimeTypeChannel:
		id, readErr := reader.ReadUvarint()
		if readErr != nil {
			return nil, readErr
		}
		if id == 0 {
			res = &Var{VType: TypeChannel}
			break
		}
		endpoint, ok := e.channelRegistry().LookupChannel(id)
		if !ok || endpoint == nil {
			err = fmt.Errorf("unknown FFI channel endpoint %d", id)
			break
		}
		if directionErr := validateChannelEndpointDirection(typ, endpoint); directionErr != nil {
			err = directionErr
			break
		}
		elem, elemOK := typ.ReadChanElemType()
		if !elemOK {
			err = fmt.Errorf("invalid FFI channel type %s", typ.Raw)
			break
		}
		if endpointElem := strings.TrimSpace(endpoint.ElemType()); endpointElem != "" {
			if parsedElem, parseErr := ParseRuntimeType(TypeSpec(endpointElem)); parseErr == nil && !parsedElem.Raw.IsAssignableTo(elem.Raw) && !elem.Raw.IsAssignableTo(parsedElem.Raw) {
				err = fmt.Errorf("FFI channel element mismatch: endpoint %s, schema %s", parsedElem.Raw, elem.Raw)
				break
			}
		}
		res = &Var{VType: TypeChannel, Ref: NewExternalVMChannel(elem, endpoint)}
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
		raw, readErr := reader.ReadRawInterface()
		if readErr != nil {
			err = readErr
			break
		}
		res, err = e.ToVar(session, raw, bridge)
	case RuntimeTypeStruct:
		res, err = e.deserializeStructSchema(session, reader, &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: typ.Fields}, bridge)
	case RuntimeTypeFunction:
		raw, readErr := reader.ReadAny()
		if readErr != nil {
			err = readErr
			break
		}
		if err = rejectHostIdentityInAny(raw); err == nil {
			decoded, convErr := e.ToVar(session, raw, bridge)
			if convErr != nil {
				err = convErr
				break
			}
			res = e.wrapAnyVar(decoded)
		}
	default:
		return nil, fmt.Errorf("unsupported FFI return type: %s", typ.Raw)
	}

	if err != nil {
		return nil, err
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	if res != nil {
		res.SetRuntimeType(typ)
	}
	return res, nil
}

func rejectHostIdentityInAny(v interface{}) error {
	switch val := v.(type) {
	case nil:
		return nil
	case uint32:
		return errors.New("FFI Any cannot carry host reference handle")
	case ffigo.InterfaceData:
		return errors.New("FFI Any cannot carry interface")
	case ffigo.ErrorData:
		if val.Handle != 0 {
			return errors.New("FFI Any cannot carry host error handle")
		}
		return nil
	case *ffigo.VMStruct:
		for _, field := range val.Fields {
			if err := rejectHostIdentityInAny(field.Value); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		for _, item := range val {
			if err := rejectHostIdentityInAny(item); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, item := range val {
			if err := rejectHostIdentityInAny(item); err != nil {
				return err
			}
		}
	}
	return nil
}
