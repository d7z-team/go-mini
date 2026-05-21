package runtime

import (
	"fmt"
	"reflect"
	"strings"
	"weak"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) ToVar(session *StackContext, val interface{}, bridge ffigo.FFIBridge) *Var {
	if val == nil {
		return nil
	}

	// 1. 规范化处理 (处理数组、指针穿透、FFI struct 等)
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
		res = &Var{VType: TypeHostRef, Handle: v, Bridge: bridge, Ref: h}
		res.SetRawType(HostRefType(SpecAny).String())
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
			methods = append(methods, RuntimeInterfaceMethod{Name: k, Spec: methodSig})
		}
		ifaceSpec, _ := ParseRuntimeInterfaceSpec(InterfaceType(methods))

		target := &Var{VType: TypeHostRef, Handle: v.Handle, Bridge: bridge}
		target.SetRawType(HostRefType(SpecAny).String())
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
		}
		res.SetRawType("Error")
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
		fields := make([]*Slot, len(v.Fields))
		byName := make(map[string]int, len(v.Fields))
		specFields := make([]RuntimeStructField, len(v.Fields))
		for i, field := range v.Fields {
			val := e.ToVar(session, field.Value, bridge)
			fieldType := MustParseRuntimeType("Any")
			if val != nil && !val.RuntimeType().IsEmpty() {
				fieldType = val.RuntimeType()
			}
			fields[i] = NewSlot(fieldType, val)
			byName[field.Name] = i
			specFields[i] = RuntimeStructField{Name: field.Name, TypeInfo: fieldType}
		}
		spec := &RuntimeStructSpec{Spec: "struct", TypeInfo: MustParseRuntimeType("Any"), Fields: specFields}
		res = &Var{VType: TypeStruct, Ref: &VMStruct{Spec: spec, Fields: fields, ByName: byName}}
	case *ffigo.VMPointer:
		inner := e.ToVar(session, v.Value, bridge)
		res = &Var{VType: TypeHandle, Ref: NewSlot(MustParseRuntimeType("Any"), inner)}
		res.SetRawType(PtrType(SpecAny).String())
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
