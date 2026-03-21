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

		// 2. 序列化变长参数部分：[Count (Uint32)] [Item1] [Item2]...
		numVariadic := 0
		if len(args) > numNormal {
			numVariadic = len(args) - numNormal
		}
		buf.WriteUint32(uint32(numVariadic))
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

	// 呼叫 Bridge
	retData, err := route.Bridge.Call(session.Context, route.MethodID, buf.Bytes())
	if err != nil {
		return nil, err
	}

	// 解析返回值
	if len(retData) == 0 {
		return nil, nil
	}

	reader := ffigo.NewReader(retData)
	retType := ast.GoMiniType(route.Returns)

	// 检查是否是 Result<T> 类型
	if retType.IsResult() {
		status := reader.ReadByte() // 0: Success, 1: Error
		innerType, _ := retType.ReadResult()

		if status == 0 {
			val, err := e.deserializeVar(session, reader, innerType, route.Bridge)
			if err != nil {
				return nil, err
			}
			return &Var{VType: TypeResult, ResultVal: val, Type: retType}, nil
		}
		errMsg := reader.ReadString()
		return &Var{VType: TypeResult, ResultErr: errMsg, Type: retType}, nil
	}

	return e.deserializeVar(session, reader, retType, route.Bridge)
}

func (e *Executor) serializeKey(buf *ffigo.Buffer, key string, kType ast.GoMiniType) error {
	switch kType {
	case "String":
		buf.WriteString(key)
	case "Int64":
		v, _ := strconv.ParseInt(key, 10, 64)
		buf.WriteInt64(v)
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
		buf.WriteUint32(uint32(iVal))
	case typ.IsNumeric():
		iVal := int64(0)
		if v != nil {
			iVal, _ = v.ToInt()
		}
		buf.WriteInt64(iVal)
	case typ == "Bool":
		bVal := false
		if v != nil {
			bVal, _ = v.ToBool()
		}
		buf.WriteBool(bVal)
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
		buf.WriteUint32(hVal)
	case typ.IsArray():
		if v == nil || v.VType != TypeArray {
			buf.WriteUint32(0)
			return nil
		}
		arr := v.Ref.(*VMArray)
		buf.WriteUint32(uint32(len(arr.Data)))
		itemType, _ := typ.ReadArrayItemType()
		for _, item := range arr.Data {
			if err := e.serializeVar(buf, item, itemType); err != nil {
				return err
			}
		}
	case typ.IsMap():
		if v == nil || v.VType != TypeMap {
			buf.WriteUint32(0)
			return nil
		}
		kType, vType, ok := typ.GetMapKeyValueTypes()
		if ok {
			vmMap := v.Ref.(*VMMap)
			buf.WriteUint32(uint32(len(vmMap.Data)))
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
			buf.WriteUint32(v.Handle)
			return nil
		}
		// 其他情况回退到 Any 动态序列化
		e.serializeVarToAny(buf, v)
	}
	return nil
}

func (e *Executor) serializeVarToAny(buf *ffigo.Buffer, v *Var) {
	e.serializeVarToAnyWithDepth(buf, v, 0)
}

func (e *Executor) serializeVarToAnyWithDepth(buf *ffigo.Buffer, v *Var, depth int) {
	if depth > 100 {
		buf.WriteAny(nil)
		return
	}
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
	case TypeHandle:
		buf.WriteAny(v.Handle)
	case TypeArray:
		arr := v.Ref.(*VMArray)
		buf.WriteByte(ffigo.TypeTagArray)
		buf.WriteUint32(uint32(len(arr.Data)))
		for _, item := range arr.Data {
			e.serializeVarToAnyWithDepth(buf, item, depth+1)
		}
	case TypeMap:
		vmMap := v.Ref.(*VMMap)
		buf.WriteByte(ffigo.TypeTagMap)
		buf.WriteUint32(uint32(len(vmMap.Data)))
		for k, val := range vmMap.Data {
			buf.WriteString(k)
			e.serializeVarToAnyWithDepth(buf, val, depth+1)
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
		return &Var{VType: TypeHandle, Handle: v, Bridge: bridge, Ref: h}
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
	case "Int64":
		return strconv.FormatInt(reader.ReadInt64(), 10), nil
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

	if typ == "Any" {
		res = e.ToVar(session, reader.ReadAny(), bridge)
	} else {
		switch {
		case typ == "String":
			res = NewString(reader.ReadString())
		case typ == "Int64" || typ == "int" || typ == "int64":
			res = NewInt(reader.ReadInt64())
		case typ == "Uint32" || typ == "uint32" || typ == "Int32" || typ == "int32":
			res = NewInt(int64(reader.ReadUint32()))
		case typ == "Float64":
			res = NewFloat(reader.ReadFloat64())
		case typ == "Bool":
			res = NewBool(reader.ReadBool())
		case typ == "TypeBytes":
			res = &Var{VType: TypeBytes, B: reader.ReadBytes()}
		case strings.HasPrefix(string(typ), "Ptr<") || typ == "TypeHandle":
			id := reader.ReadUint32()
			var h *VMHandle
			if id != 0 {
				h = NewVMHandle(id, bridge)
				session.AddHandle(bridge, id)
			}
			res = &Var{VType: TypeHandle, Handle: id, Bridge: bridge, Ref: h}
		case typ.IsArray():
			count := int(reader.ReadUint32())
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
			count := int(reader.ReadUint32())
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
				// Use ReadAny/ToVar to handle tagged data from bridge
				val := e.ToVar(session, reader.ReadAny(), bridge)
				if val != nil {
					val.Type = t
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
			// Fallback: If it's an unknown named type, assume it's an opaque handle (Uint32)
			if reader.Available() >= 4 {
				id := reader.ReadUint32()
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
