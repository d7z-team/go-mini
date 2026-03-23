package runtime

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) evalFFI(session *StackContext, route FFIRoute, args []*Var) (*Var, error) {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	// 获取函数签名以获取参数类型列表
	fn, ok := ast.GoMiniType(route.Spec).ReadCallFunc()

	// 序列化参数
	if ok && fn.Variadic {
		// 1. 序列化常规参数
		numNormal := len(fn.Params) - 1
		for i := 0; i < numNormal; i++ {
			arg := &Var{VType: TypeAny} // 默认
			if i < len(args) {
				arg = args[i]
			}
			if err := e.serializeVar(buf, arg, fn.Params[i]); err != nil {
				return nil, err
			}
		}

		// 2. 序列化变长参数部分：[Count (Uvarint)] [Item1] [Item2]...
		numVariadic := 0
		if len(args) > numNormal {
			numVariadic = len(args) - numNormal
		}
		buf.WriteUvarint(uint64(numVariadic))
		itemType := fn.Params[numNormal]
		if numVariadic > 0 {
			for i := 0; i < numVariadic; i++ {
				if err := e.serializeVar(buf, args[numNormal+i], itemType); err != nil {
					return nil, err
				}
			}
		}
	} else {
		// 普通非变长函数序列化
		for i, arg := range args {
			var argType ast.GoMiniType = "Any"
			if ok && i < len(fn.Params) {
				argType = fn.Params[i]
			}
			if err := e.serializeVar(buf, arg, argType); err != nil {
				return nil, err
			}
		}
	}

	// 发起 FFI 调用
	var retData []byte
	var err error

	// 硬编码拦截内置扩展路由
	if route.MethodID == 999 && route.Name == "errors.is" {
		if len(args) == 2 {
			errVar := args[0]
			targetHandle := args[1]
			match := false
			if errVar != nil && errVar.VType == TypeError {
				if errObj, ok := errVar.Ref.(*VMError); ok {
					hVal, _ := targetHandle.ToHandle()
					match = errObj.Handle == hVal
				}
			}
			return NewBool(match), nil
		}
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("FFI panic: %v", r)
				err = &VMError{Value: NewString(msg), Message: msg, IsPanic: true}
			}
		}()
		if route.MethodID == 0 {
			retData, err = route.Bridge.Invoke(session.Context, route.Name, buf.Bytes())
		} else {
			retData, err = route.Bridge.Call(session.Context, route.MethodID, buf.Bytes())
		}
	}()

	if err != nil {
		if vme, ok := err.(*VMError); ok {
			vme.IsPanic = true
			return nil, vme
		}
		// 将宿主 Error 包装为 Panic
		return nil, &VMError{
			Message: err.Error(),
			Value:   NewString(err.Error()),
			IsPanic: true,
			Cause:   err,
		}
	}

	// 解析返回值
	if len(retData) == 0 {
		return nil, nil
	}

	reader := ffigo.NewReader(retData)
	retType := ast.GoMiniType(route.Returns)

	// 检查是否是 Tuple
	if retType.IsTuple() {
		types, _ := retType.ReadTuple()
		tupleData := make([]*Var, len(types))
		for i, t := range types {
			val, err := e.deserializeVar(session, reader, t, route.Bridge)
			if err != nil {
				return nil, err
			}
			tupleData[i] = val
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: tupleData}, Type: retType}, nil
	}

	return e.deserializeVar(session, reader, retType, route.Bridge)
}

func (e *Executor) serializeKey(buf *ffigo.Buffer, key string, kType ast.GoMiniType) error {
	switch kType {
	case "String":
		buf.WriteString(key)
	case "Int64", "Int", "int", "int64":
		v, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse map key '%s' as Int64: %w", key, err)
		}
		buf.WriteVarint(v)
	case "Bool", "bool":
		v, err := strconv.ParseBool(key)
		if err != nil {
			return fmt.Errorf("failed to parse map key '%s' as Bool: %w", key, err)
		}
		buf.WriteBool(v)
	case "Float64", "float64":
		v, err := strconv.ParseFloat(key, 64)
		if err != nil {
			return fmt.Errorf("failed to parse map key '%s' as Float64: %w", key, err)
		}
		buf.WriteFloat64(v)
	default:
		return fmt.Errorf("unsupported map key type: %s", kType)
	}
	return nil
}

func (e *Executor) serializeVar(buf *ffigo.Buffer, v *Var, typ ast.GoMiniType) error {
	// 如果 typ 是 Any，回退到动态序列化
	if typ == "Any" {
		e.serializeVarToAny(buf, v)
		return nil
	}

	// 严格按照 typ 类型进行序列化，防止协议层错位
	switch {
	case typ == "String":
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
	case typ == "Float64":
		fVal := 0.0
		if v != nil {
			fVal, _ = v.ToFloat()
		}
		buf.WriteFloat64(fVal)
	case typ == "Uint32" || typ == "uint32" || typ == "Int32" || typ == "int32":
		iVal := int64(0)
		if v != nil {
			iVal, _ = v.ToInt()
		}
		buf.WriteUvarint(uint64(iVal))
	case typ.IsNumeric():
		iVal := int64(0)
		if v != nil {
			iVal, _ = v.ToInt()
		}
		buf.WriteVarint(iVal)
	case typ == "Bool":
		bVal := false
		if v != nil {
			bVal, _ = v.ToBool()
		}
		buf.WriteBool(bVal)
	case typ == "Error" || typ == "error":
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
	case typ == "TypeBytes":
		var bVal []byte
		if v != nil {
			bVal, _ = v.ToBytes()
		}
		buf.WriteBytes(bVal)
	case typ.IsPtr() || typ == "TypeHandle":
		hVal := uint32(0)
		if v != nil {
			hVal, _ = v.ToHandle()
		}
		buf.WriteUvarint(uint64(hVal))
	case typ.IsArray():
		if v == nil || v.VType != TypeArray {
			buf.WriteUvarint(0)
			return nil
		}
		arr := v.Ref.(*VMArray)
		buf.WriteUvarint(uint64(len(arr.Data)))
		itemType, _ := typ.ReadArrayItemType()
		for _, item := range arr.Data {
			if err := e.serializeVar(buf, item, itemType); err != nil {
				return err
			}
		}
	case typ.IsInterface():
		if v == nil || v.VType != TypeInterface || v.Ref == nil {
			buf.WriteRawInterface(0, nil)
			return nil
		}
		if iface, ok := v.Ref.(*VMInterface); ok {
			methods := make(map[string]string)
			for k, v := range iface.Methods {
				methods[k] = v.String()
			}
			buf.WriteRawInterface(iface.Target.Handle, methods)
		} else {
			buf.WriteRawInterface(0, nil)
		}
	case typ.IsMap():
		if v == nil || v.VType != TypeMap {
			buf.WriteUvarint(0)
			return nil
		}
		kType, vType, ok := typ.GetMapKeyValueTypes()
		if ok {
			vmMap := v.Ref.(*VMMap)
			buf.WriteUvarint(uint64(len(vmMap.Data)))
			for k, val := range vmMap.Data {
				if err := e.serializeKey(buf, k, kType); err != nil {
					return err
				}
				if err := e.serializeVar(buf, val, vType); err != nil {
					return err
				}
			}
		}
	case typ.IsTuple():
		types, _ := typ.ReadTuple()
		if v == nil || v.VType != TypeArray {
			for range types {
				buf.WriteAny(nil)
			}
			return nil
		}
		arr := v.Ref.(*VMArray)
		for i, t := range types {
			var arg *Var
			if i < len(arr.Data) {
				arg = arr.Data[i]
			}
			if err := e.serializeVar(buf, arg, t); err != nil {
				return err
			}
		}
	default:
		// 结构体序列化
		if name, ok := typ.StructName(); ok {
			if sDef, ok := e.program.Structs[name]; ok {
				var mData map[string]*Var
				if v != nil && v.VType == TypeMap {
					mData = v.Ref.(*VMMap).Data
				}
				for _, fName := range sDef.FieldNames {
					fType := sDef.Fields[fName]
					var fVal *Var
					if mData != nil {
						fVal = mData[string(fName)]
					}
					if err := e.serializeVar(buf, fVal, fType); err != nil {
						return err
					}
				}
				return nil
			}
		}
		// Custom named types (like 'Order') that are handles
		if v != nil && v.VType == TypeHandle {
			buf.WriteUvarint(uint64(v.Handle))
			return nil
		}
		// 其他情况回退到 Any 动态序列化
		e.serializeVarToAny(buf, v)
	}
	return nil
}

func (e *Executor) serializeVarToAny(buf *ffigo.Buffer, v *Var) {
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
			buf.WriteByte(ffigo.TypeTagError)
			buf.WriteRawError(err.Message, err.Handle)
		} else {
			buf.WriteAny(nil)
		}
	case TypeHandle:
		if v.Bridge == nil && v.Ref != nil {
			if inner, ok := v.Ref.(*Var); ok {
				buf.WriteByte(ffigo.TypeTagPointer)
				e.serializeVarToAny(buf, inner)
				return
			}
		}
		buf.WriteAny(v.Handle)
	case TypeArray:
		arr := v.Ref.(*VMArray)
		buf.WriteByte(ffigo.TypeTagArray)
		buf.WriteUvarint(uint64(len(arr.Data)))
		for _, item := range arr.Data {
			e.serializeVarToAny(buf, item)
		}
	case TypeMap:
		vmMap := v.Ref.(*VMMap)
		// 启发式判断：如果有具体类型且不是 Map<，则视为 Struct
		isStruct := !v.Type.IsEmpty() && !v.Type.IsMap() && v.Type != "Any"
		if isStruct {
			buf.WriteByte(ffigo.TypeTagStruct)
		} else {
			buf.WriteByte(ffigo.TypeTagMap)
		}
		buf.WriteUvarint(uint64(len(vmMap.Data)))
		for k, val := range vmMap.Data {
			buf.WriteString(k)
			e.serializeVarToAny(buf, val)
		}
	case TypeInterface:
		if v.Ref == nil {
			buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(0, nil)
			return
		}
		if iface, ok := v.Ref.(*VMInterface); ok {
			methods := make(map[string]string)
			for k, v := range iface.Methods {
				methods[k] = v.String()
			}
			buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(iface.Target.Handle, methods)
		} else {
			buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(0, nil)
		}
	default:
		buf.WriteAny(nil)
	}
}

func (e *Executor) ToVar(session *StackContext, val interface{}, bridge ffigo.FFIBridge) *Var {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case *Var:
		return v
	case int:
		return NewInt(int64(v))
	case int64:
		return NewInt(v)
	case float64:
		return NewFloat(v)
	case string:
		return NewString(v)
	case []byte:
		if v == nil {
			return nil
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		return NewBytes(buf)
	case bool:
		return NewBool(v)
	case uint32:
		var h *VMHandle
		if v != 0 {
			h = NewVMHandle(v, bridge)
			session.AddHandle(bridge, v)
		}
		return &Var{VType: TypeHandle, Handle: v, Bridge: bridge, Ref: h, Type: "TypeHandle"}
	case ffigo.InterfaceData:
		var ifaceStr strings.Builder
		ifaceStr.WriteString("interface{")
		for k, sig := range v.Methods {
			// 简单的安全性过滤：方法名不能包含特殊字符
			if strings.ContainsAny(k, "{};() ") {
				continue
			}
			ifaceStr.WriteString(k)
			if strings.HasPrefix(sig, "function(") {
				ifaceStr.WriteString(strings.TrimPrefix(sig, "function"))
			} else {
				ifaceStr.WriteString(sig)
			}
			ifaceStr.WriteString(";")
		}
		ifaceStr.WriteString("}")
		methods, _ := ast.GoMiniType(ifaceStr.String()).ReadInterfaceMethods()

		target := &Var{VType: TypeHandle, Handle: v.Handle, Bridge: bridge, Type: "TypeHandle"}
		if v.Handle != 0 {
			target.Ref = NewVMHandle(v.Handle, bridge)
			session.AddHandle(bridge, v.Handle)
		}
		return &Var{
			VType: TypeInterface,
			Ref: &VMInterface{
				Target:  target,
				Methods: methods,
			},
			Bridge: bridge,
		}
	case ffigo.ErrorData:
		if v.Message == "" && v.Handle == 0 {
			return nil
		}
		errObj := &VMError{
			Message: v.Message,
			Handle:  v.Handle,
			Bridge:  bridge,
		}
		if v.Handle != 0 {
			session.AddHandle(bridge, v.Handle)
		}
		return &Var{
			VType:  TypeError,
			Ref:    errObj,
			Bridge: bridge,
			Handle: v.Handle,
			Type:   "Error",
		}
	case map[string]interface{}:
		res := make(map[string]*Var)
		for k, raw := range v {
			res[k] = e.ToVar(session, raw, bridge)
		}
		return &Var{VType: TypeMap, Ref: &VMMap{Data: res}}
	case []interface{}:
		res := make([]*Var, len(v))
		for i, raw := range v {
			res[i] = e.ToVar(session, raw, bridge)
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: res}}
	default:
		return &Var{VType: TypeAny, Ref: v}
	}
}

func (e *Executor) deserializeKey(reader *ffigo.Reader, kType ast.GoMiniType) (string, error) {
	switch kType {
	case "String":
		return reader.ReadString(), nil
	case "Int64", "Int", "int", "int64":
		return strconv.FormatInt(reader.ReadVarint(), 10), nil
	case "Bool", "bool":
		return strconv.FormatBool(reader.ReadBool()), nil
	case "Float64", "float64":
		return strconv.FormatFloat(reader.ReadFloat64(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported map key type: %s", kType)
	}
}

func (e *Executor) deserializeVar(session *StackContext, reader *ffigo.Reader, typ ast.GoMiniType, bridge ffigo.FFIBridge) (*Var, error) {
	if typ.IsVoid() {
		return nil, nil
	}
	if reader.Available() == 0 {
		return nil, nil
	}

	var res *Var
	var err error

	// 规范化类型字符串，防止因空格或不可见字符导致的匹配失败
	typ = ast.GoMiniType(strings.TrimSpace(string(typ)))

	if typ == "Any" {
		res = e.ToVar(session, reader.ReadAny(), bridge)
	} else {
		switch {
		case typ == "String":
			res = NewString(reader.ReadString())
		case typ == "Int64" || typ == "int" || typ == "int64":
			res = NewInt(reader.ReadVarint())
		case typ == "Uint32" || typ == "uint32" || typ == "Int32" || typ == "int32":
			res = NewInt(int64(reader.ReadUvarint()))
		case typ == "Float64":
			res = NewFloat(reader.ReadFloat64())
		case typ == "Bool":
			res = NewBool(reader.ReadBool())
		case typ == "Error" || typ == "error":
			res = e.ToVar(session, reader.ReadRawError(), bridge)
		case typ == "TypeBytes":
			res = &Var{VType: TypeBytes, B: reader.ReadBytes()}
		case typ.IsPtr() || typ == "TypeHandle":
			id := uint32(reader.ReadUvarint())
			var h *VMHandle
			if id != 0 {
				h = NewVMHandle(id, bridge)
				session.AddHandle(bridge, id)
			}
			res = &Var{VType: TypeHandle, Handle: id, Bridge: bridge, Ref: h}
		case typ.IsArray():
			count := int(reader.ReadUvarint())
			itemType, _ := typ.ReadArrayItemType()
			arrData := make([]*Var, count)
			for i := 0; i < count; i++ {
				val, err := e.deserializeVar(session, reader, itemType, bridge)
				if err != nil {
					return nil, err
				}
				arrData[i] = val
			}
			res = &Var{VType: TypeArray, Ref: &VMArray{Data: arrData}}
		case typ.IsMap():
			count := int(reader.ReadUvarint())
			kType, vType, _ := typ.GetMapKeyValueTypes()
			mapData := make(map[string]*Var)
			for i := 0; i < count; i++ {
				k, err := e.deserializeKey(reader, kType)
				if err != nil {
					return nil, err
				}
				val, err := e.deserializeVar(session, reader, vType, bridge)
				if err != nil {
					return nil, err
				}
				mapData[k] = val
			}
			res = &Var{VType: TypeMap, Ref: &VMMap{Data: mapData}}
		case typ.IsTuple():
			types, _ := typ.ReadTuple()
			tupleData := make([]*Var, len(types))
			for i, t := range types {
				val, err := e.deserializeVar(session, reader, t, bridge)
				if err != nil {
					return nil, err
				}
				tupleData[i] = val
			}
			res = &Var{VType: TypeArray, Ref: &VMArray{Data: tupleData}}
		default:
			if name, ok := typ.StructName(); ok {
				if sDef, ok := e.program.Structs[name]; ok {
					resMap := make(map[string]*Var)
					for _, fName := range sDef.FieldNames {
						fType := sDef.Fields[fName]
						val, err := e.deserializeVar(session, reader, fType, bridge)
						if err != nil {
							return nil, err
						}
						resMap[string(fName)] = val
					}
					res = &Var{VType: TypeMap, Ref: &VMMap{Data: resMap}}
					break
				}
			}
			// Fallback: If it's an unknown named type, assume it's an opaque handle
			if reader.Available() > 0 {
				id := uint32(reader.ReadUvarint())
				var h *VMHandle
				if id != 0 {
					h = NewVMHandle(id, bridge)
					session.AddHandle(bridge, id)
				}
				res = &Var{VType: TypeHandle, Handle: id, Bridge: bridge, Ref: h}
				break
			}
			return nil, fmt.Errorf("unsupported FFI return type: %s", typ)
		}
	}

	if res != nil {
		res.Type = typ
	}
	return res, err
}
