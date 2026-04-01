package runtime

import (
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"weak"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) evalFFI(session *StackContext, route FFIRoute, args []*Var) (*Var, error) {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	funcSig := route.FuncSig
	if funcSig == nil && route.Spec != "" {
		funcSig, _ = ParseRuntimeFuncSig(ast.GoMiniType(route.Spec))
	}

	// 序列化参数
	if funcSig != nil && funcSig.Function.Variadic {
		// 1. 序列化常规参数
		numNormal := len(funcSig.ParamTypes) - 1
		for i := 0; i < numNormal; i++ {
			arg := &Var{VType: TypeAny} // 默认
			if i < len(args) {
				arg = args[i]
			}
			if err := e.serializeRuntimeType(buf, arg, funcSig.ParamTypes[i]); err != nil {
				return nil, err
			}
		}

		// 2. 序列化变长参数部分：[Count (Uvarint)] [Item1] [Item2]...
		numVariadic := 0
		if len(args) > numNormal {
			numVariadic = len(args) - numNormal
		}
		buf.WriteUvarint(uint64(numVariadic))
		itemType := funcSig.ParamTypes[numNormal]
		if numVariadic > 0 {
			for i := 0; i < numVariadic; i++ {
				if err := e.serializeRuntimeType(buf, args[numNormal+i], itemType); err != nil {
					return nil, err
				}
			}
		}
	} else {
		// 普通非变长函数序列化
		for i, arg := range args {
			argType := RuntimeType{Kind: RuntimeTypeAny, Raw: ast.TypeAny, TypeID: CanonicalTypeID(string(ast.TypeAny))}
			if funcSig != nil && i < len(funcSig.ParamTypes) {
				argType = funcSig.ParamTypes[i]
			}
			if err := e.serializeRuntimeType(buf, arg, argType); err != nil {
				return nil, err
			}
		}
	}

	// 发起 FFI 调用
	var retData []byte
	var err error

	// 硬编码拦截内置扩展路由
	if route.MethodID == 999999999 && route.Name == "errors.Is" {
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
		runtime.KeepAlive(args) // 关键：确保参数在调用期间不被回收
	}()

	if err != nil {
		if vme, ok := err.(*VMError); ok {
			vme.IsPanic = true
			return nil, vme
		}

		// 获取 VM 调用栈
		frames := session.GenerateStackTrace(nil)
		var stackStr strings.Builder
		for i, f := range frames {
			fmt.Fprintf(&stackStr, "\n  #%d %s (%s:%d:%d)", i, f.Function, f.Filename, f.Line, f.Column)
		}

		// 将宿主 Error 包装为带栈信息的 Panic
		return nil, &VMError{
			Message: fmt.Sprintf("%v\n\nVM Stack Trace:%s", err.Error(), stackStr.String()),
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
	if funcSig != nil {
		return e.deserializeRuntimeType(session, reader, funcSig.ReturnType, route.Bridge)
	}
	retType, _ := ParseRuntimeType(route.Return)
	return e.deserializeRuntimeType(session, reader, retType, route.Bridge)
}

func (e *Executor) serializeRuntimeType(buf *ffigo.Buffer, v *Var, typ RuntimeType) error {
	switch typ.Kind {
	case RuntimeTypeVoid:
		return nil
	case RuntimeTypeAny:
		e.serializeVarToAny(buf, v)
		return nil
	case RuntimeTypePrimitive, RuntimeTypePointer, RuntimeTypeArray, RuntimeTypeMap, RuntimeTypeTuple, RuntimeTypeFunction:
		return e.serializeVar(buf, v, typ.Raw)
	case RuntimeTypeInterface:
		if v == nil || v.VType != TypeInterface || v.Ref == nil {
			buf.WriteRawInterface(0, nil)
			return nil
		}
		if iface, ok := v.Ref.(*VMInterface); ok {
			buf.WriteRawInterface(iface.Target.Handle, iface.Spec.MethodStringMap())
			return nil
		}
		buf.WriteRawInterface(0, nil)
		return nil
	case RuntimeTypeStruct:
		return e.serializeStructSchema(buf, v, &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: typ.Fields})
	case RuntimeTypeNamed:
		if schema, ok := e.structSchemas[typ.TypeID]; ok {
			return e.serializeStructSchema(buf, v, schema)
		}
		return e.serializeVar(buf, v, typ.Raw)
	default:
		return e.serializeVar(buf, v, typ.Raw)
	}
}

func (e *Executor) deserializeRuntimeType(session *StackContext, reader *ffigo.Reader, typ RuntimeType, bridge ffigo.FFIBridge) (*Var, error) {
	res, err := e.deserializeParsedType(session, reader, typ, bridge)
	if res != nil {
		res.Type = typ.Raw
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
	var mData map[string]*Var
	if v != nil && v.VType == TypeMap {
		mData = v.Ref.(*VMMap).Data
	}
	for _, field := range schema.Fields {
		var fVal *Var
		if mData != nil {
			fVal = mData[field.Name]
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
	resMap := make(map[string]*Var, len(schema.Fields))
	for _, field := range schema.Fields {
		val, err := e.deserializeRuntimeType(session, reader, field.TypeInfo, bridge)
		if err != nil {
			return nil, err
		}
		resMap[field.Name] = val
	}
	return &Var{VType: TypeMap, Ref: &VMMap{Data: resMap}, Type: schema.Spec}, nil
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

func (e *Executor) lookupStructSchema(typ RuntimeType) (*RuntimeStructSpec, bool) {
	if schema, ok := e.structSchemas[typ.TypeID]; ok {
		return schema, true
	}
	if schema, ok := e.structSchemas[string(typ.Raw)]; ok {
		return schema, true
	}
	if typ.TypeID == "" {
		return nil, false
	}
	var matched *RuntimeStructSpec
	for key, schema := range e.structSchemas {
		if key == typ.TypeID || strings.HasSuffix(key, "."+typ.TypeID) || strings.HasSuffix(key, "/"+typ.TypeID) {
			if matched != nil {
				return nil, false
			}
			matched = schema
		}
	}
	if matched != nil {
		return matched, true
	}
	return nil, false
}

func (e *Executor) serializeParsedType(buf *ffigo.Buffer, v *Var, typ RuntimeType) error {
	if v != nil && v.VType == TypeCell && v.Ref != nil {
		if cell, ok := v.Ref.(*Cell); ok {
			v = cell.Value
		}
	}
	if v != nil && v.VType == TypeAny && v.Ref != nil {
		if inner, ok := v.Ref.(*Var); ok {
			v = inner
		}
	}

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
			return e.serializeStructSchema(buf, v, schema)
		}
		if v != nil && v.VType == TypeHandle {
			buf.WriteUvarint(uint64(v.Handle))
			return nil
		}
		e.serializeVarToAny(buf, v)
		return nil
	case RuntimeTypePointer:
		hVal := uint32(0)
		if v != nil {
			hVal, _ = v.ToHandle()
		}
		buf.WriteUvarint(uint64(hVal))
		return nil
	case RuntimeTypeArray:
		if v == nil || v.VType != TypeArray {
			buf.WriteUvarint(0)
			return nil
		}
		arr := v.Ref.(*VMArray)
		buf.WriteUvarint(uint64(len(arr.Data)))
		for _, item := range arr.Data {
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
		buf.WriteUvarint(uint64(len(vmMap.Data)))
		for k, val := range vmMap.Data {
			if err := e.serializeKey(buf, k, typ.Key.Raw); err != nil {
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
		for i, t := range typ.Params {
			var arg *Var
			if i < len(arr.Data) {
				arg = arr.Data[i]
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

func (e *Executor) serializeVar(buf *ffigo.Buffer, v *Var, typ ast.GoMiniType) error {
	typeInfo, err := ParseRuntimeType(typ)
	if err != nil {
		if v != nil && v.VType == TypeHandle {
			buf.WriteUvarint(uint64(v.Handle))
			return nil
		}
		e.serializeVarToAny(buf, v)
		return nil
	}
	return e.serializeParsedType(buf, v, typeInfo)
}

func (e *Executor) serializeVarToAny(buf *ffigo.Buffer, v *Var) {
	if v == nil {
		buf.WriteAny(nil)
		return
	}
	switch v.VType {
	case TypeAny:
		if v.Ref == nil {
			buf.WriteAny(nil)
			return
		}
		if inner, ok := v.Ref.(*Var); ok {
			e.serializeVarToAny(buf, inner)
			return
		}
		buf.WriteAny(v.Ref)
		return
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
		// Internal VM pointers travel as pointer-tagged Any values.
		if v.Bridge == nil && v.Ref != nil {
			if inner, ok := v.Ref.(*Var); ok {
				buf.WriteByte(ffigo.TypeTagPointer)
				e.serializeVarToAny(buf, inner)
				return
			}
		}
		// Host-visible handles always travel as opaque handle IDs.
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
		if schema, ok := e.lookupAnyStructSchema(v); ok {
			buf.WriteByte(ffigo.TypeTagStruct)
			buf.WriteUvarint(uint64(len(schema.Fields)))
			for _, field := range schema.Fields {
				buf.WriteString(field.Name)
				e.serializeVarToAny(buf, vmMap.Data[field.Name])
			}
			return
		}
		buf.WriteByte(ffigo.TypeTagMap)
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
			buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(iface.Target.Handle, iface.Spec.MethodStringMap())
		} else {
			buf.WriteByte(ffigo.TypeTagInterface)
			buf.WriteRawInterface(0, nil)
		}
	default:
		buf.WriteAny(nil)
	}
}

func (e *Executor) lookupAnyStructSchema(v *Var) (*RuntimeStructSpec, bool) {
	if v == nil || v.Type.IsEmpty() || v.Type.IsMap() || v.Type == ast.TypeAny {
		return nil, false
	}
	typeInfo, err := ParseRuntimeType(v.Type)
	if err != nil {
		return nil, false
	}
	if typeInfo.Kind == RuntimeTypeStruct || typeInfo.Kind == RuntimeTypeNamed {
		return e.lookupStructSchema(typeInfo)
	}
	return nil, false
}

func (e *Executor) ToVar(session *StackContext, val interface{}, bridge ffigo.FFIBridge) *Var {
	if val == nil {
		return nil
	}

	// 1. 规范化处理 (处理数组崩溃、指针穿透、Struct转Map等)
	norm, err := e.normalizeValue(val)
	if err != nil {
		// 规范化失败时，为了安全起见返回 Any 包装的原始值或 Nil
		return &Var{VType: TypeAny, Ref: val}
	}
	val = norm

	// 2. 核心转换逻辑
	var res *Var
	switch v := val.(type) {
	case *Var:
		res = v
	case int:
		res = NewInt(int64(v))
	case int64:
		res = NewInt(v)
	case float64:
		res = NewFloat(v)
	case string:
		res = NewString(v)
	case []byte:
		if v == nil {
			return nil
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		res = NewBytes(buf)
	case bool:
		res = NewBool(v)
	case uint32:
		var h *VMHandle
		if v != 0 {
			h = NewVMHandle(v, bridge)
		}
		res = &Var{VType: TypeHandle, Handle: v, Bridge: bridge, Ref: h, Type: "TypeHandle"}
	case ffigo.InterfaceData:
		var ifaceStr strings.Builder
		ifaceStr.WriteString("interface{")
		for k, sig := range v.Methods {
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
		ifaceSpec, _ := ParseRuntimeInterfaceSpec(ast.GoMiniType(ifaceStr.String()))

		target := &Var{VType: TypeHandle, Handle: v.Handle, Bridge: bridge, Type: "TypeHandle"}
		if v.Handle != 0 {
			h := NewVMHandle(v.Handle, bridge)
			target.Ref = h
		}
		res = &Var{
			VType: TypeInterface,
			Ref: &VMInterface{
				Target: target,
				Spec:   ifaceSpec,
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
			NewVMHandle(v.Handle, bridge)
		}
		res = &Var{
			VType:  TypeError,
			Ref:    errObj,
			Bridge: bridge,
			Handle: v.Handle,
			Type:   "Error",
		}
	case map[string]interface{}:
		resMap := make(map[string]*Var)
		for k, raw := range v {
			resMap[k] = e.ToVar(session, raw, bridge)
		}
		res = &Var{VType: TypeMap, Ref: &VMMap{Data: resMap}}
	case []interface{}:
		resArr := make([]*Var, len(v))
		for i, raw := range v {
			resArr[i] = e.ToVar(session, raw, bridge)
		}
		res = &Var{VType: TypeArray, Ref: &VMArray{Data: resArr}}
	case *ffigo.VMStruct:
		resMap := make(map[string]*Var, len(v.Fields))
		for _, field := range v.Fields {
			resMap[field.Name] = e.ToVar(session, field.Value, bridge)
		}
		res = &Var{VType: TypeMap, Ref: &VMMap{Data: resMap}}
	case *ffigo.VMPointer:
		inner := e.ToVar(session, v.Value, bridge)
		res = &Var{VType: TypeHandle, Type: "Ptr<Any>", Bridge: bridge, Ref: inner}
	default:
		res = &Var{VType: TypeAny, Ref: v}
	}

	if res != nil && session != nil && session.Stack != nil {
		res.stack = weak.Make(session.Stack)
	}
	return res
}

func (e *Executor) wrapAnyVar(session *StackContext, inner *Var) *Var {
	if inner == nil {
		return nil
	}
	if inner.VType == TypeAny {
		return inner
	}
	res := &Var{
		VType:  TypeAny,
		Type:   ast.TypeAny,
		Ref:    inner,
		Bridge: inner.Bridge,
		Handle: inner.Handle,
	}
	if session != nil && session.Stack != nil {
		res.stack = weak.Make(session.Stack)
	}
	return res
}

// normalizeValue 将复杂的宿主对象（如 struct）规范化为 VM 可直接处理的类型（map/slice/primitives）。
func (e *Executor) normalizeValue(val interface{}) (interface{}, error) {
	if val == nil {
		return nil, nil
	}

	// 检查是否已经是引擎原生变量，若是则直接穿透
	if _, ok := val.(*Var); ok {
		return val, nil
	}

	// 穿透支持 FFI 基础数据结构
	if _, ok := val.(ffigo.InterfaceData); ok {
		return val, nil
	}
	if _, ok := val.(ffigo.ErrorData); ok {
		return val, nil
	}
	if _, ok := val.(*ffigo.VMStruct); ok {
		return val, nil
	}
	if _, ok := val.(*ffigo.VMPointer); ok {
		return val, nil
	}

	v := reflect.ValueOf(val)
	// 处理指针：自动追踪到基元值
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
		// 注意：uint64 可能会在转 int64 时溢出，但符合 go-mini 降维映射原则
		return int64(v.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Slice, reflect.Array:
		// 特殊处理 []byte 和 [N]byte
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if v.Kind() == reflect.Slice {
				return v.Bytes(), nil
			}
			// 对于不可寻址的数组，执行手动拷贝
			res := make([]byte, v.Len())
			for i := 0; i < v.Len(); i++ {
				res[i] = uint8(v.Index(i).Uint())
			}
			return res, nil
		}
		res := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			var err error
			res[i], err = e.normalizeValue(v.Index(i).Interface())
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	case reflect.Map:
		res := make(map[string]interface{})
		for _, key := range v.MapKeys() {
			if key.Kind() != reflect.String {
				return nil, fmt.Errorf("不支持非字符串类型的 Map Key: %v", key.Kind())
			}
			var err error
			res[key.String()], err = e.normalizeValue(v.MapIndex(key).Interface())
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	case reflect.Struct:
		res := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			// 仅处理导出的字段
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			// 优先使用 json 标签作为键名 (保持与原有逻辑一致)
			if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
				name = strings.Split(tag, ",")[0]
			}
			var err error
			res[name], err = e.normalizeValue(v.Field(i).Interface())
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	default:
		// 其他类型作为 Any 穿透，由 ToVar 进一步处理
		return val, nil
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
				res, err = e.deserializeStructSchema(session, reader, schema, bridge)
				break
			}
			id := uint32(reader.ReadUvarint())
			var h *VMHandle
			if id != 0 {
				h = NewVMHandle(id, bridge)
			}
			res = &Var{VType: TypeHandle, Handle: id, Bridge: bridge, Ref: h}
		}
	case RuntimeTypePointer:
		id := uint32(reader.ReadUvarint())
		var h *VMHandle
		if id != 0 {
			h = NewVMHandle(id, bridge)
		}
		res = &Var{VType: TypeHandle, Handle: id, Bridge: bridge, Ref: h}
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
			k, err := e.deserializeKey(reader, typ.Key.Raw)
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
		res.Type = typ.Raw
		if session != nil && session.Stack != nil {
			res.stack = weak.Make(session.Stack)
		}
	}
	return res, nil
}

func (e *Executor) deserializeVar(session *StackContext, reader *ffigo.Reader, typ ast.GoMiniType, bridge ffigo.FFIBridge) (*Var, error) {
	typ = ast.GoMiniType(strings.TrimSpace(string(typ)))
	typeInfo, err := ParseRuntimeType(typ)
	if err != nil {
		return nil, err
	}
	res, err := e.deserializeParsedType(session, reader, typeInfo, bridge)
	if res != nil {
		res.Type = typ
		if session.Stack != nil {
			res.stack = weak.Make(session.Stack)
		}
	}
	return res, err
}
