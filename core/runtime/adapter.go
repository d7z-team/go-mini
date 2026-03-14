package runtime

import (
	"errors"
	"fmt"
	"reflect"

	"gopkg.d7z.net/go-mini/core/ast"
)

// runtimeArray 实现 ast.MiniArray
type runtimeArray struct {
	data     *[]any
	elemType ast.GoMiniType
}

func (r *runtimeArray) GoMiniType() ast.Ident {
	return ast.Ident(ast.CreateArrayType(r.elemType))
}

func (r *runtimeArray) Len() int {
	return len(*r.data)
}

// arrayElementProxy 处理数组元素的副作用
type arrayElementProxy struct {
	ast.MiniObj
	arr   *runtimeArray
	index int
}

func (p *arrayElementProxy) Set(val any) {
	// 尝试将 val 转换为 MiniObj 并存回数组
	m, _ := toMiniObj(val)
	if m != nil {
		_ = p.arr.Set(p.index, m)
	}
}

func (p *arrayElementProxy) Unbox() ast.MiniObj {
	return p.MiniObj
}

func (r *runtimeArray) Get(index int) (ast.MiniObj, error) {
	if index < 0 || index >= len(*r.data) {
		return nil, errors.New("index out of bounds")
	}
	val := (*r.data)[index]
	m, err := toMiniObj(val)
	if err != nil {
		return nil, err
	}
	return &arrayElementProxy{MiniObj: m, arr: r, index: index}, nil
}

// structFieldProxy 处理结构体字段的副作用
type structFieldProxy struct {
	ast.MiniObj
	stru ast.MiniStruct
	name string
}

func (p *structFieldProxy) Set(val any) {
	m, _ := toMiniObj(val)
	if m != nil {
		_ = p.stru.SetField(p.name, m)
	}
}

func (p *structFieldProxy) Unbox() ast.MiniObj {
	return p.MiniObj
}

func (r *runtimeArray) Set(index int, val ast.MiniObj) error {
	if index < 0 || index >= len(*r.data) {
		return errors.New("index out of bounds")
	}
	// 简单的类型安全检查 (如果 ElemType 不是 Any)
	if r.elemType != "" && r.elemType != "Any" && val != nil {
		if val.GoMiniType() != ast.Ident(r.elemType) {
			return fmt.Errorf("type mismatch: expected %s, got %s", r.elemType, val.GoMiniType())
		}
	}
	(*r.data)[index] = val
	return nil
}

func (r *runtimeArray) Append(val ast.MiniObj) error {
	*r.data = append(*r.data, val)
	return nil
}

func (r *runtimeArray) ElemType() ast.GoMiniType {
	return r.elemType
}

// runtimeMap 实现 ast.MiniMap
type runtimeMap struct {
	data    map[any]any
	keyType ast.GoMiniType
	valType ast.GoMiniType
}

func (r *runtimeMap) GoMiniType() ast.Ident {
	return ast.Ident(ast.CreateMapType(r.keyType, r.valType))
}

func (r *runtimeMap) Len() int {
	return len(r.data)
}

// mapValueProxy 处理 Map 值的副作用
type mapValueProxy struct {
	ast.MiniObj
	m   *runtimeMap
	key ast.MiniObj
}

func (p *mapValueProxy) Set(val any) {
	m, _ := toMiniObj(val)
	if m != nil {
		_ = p.m.Set(p.key, m)
	}
}

func (p *mapValueProxy) Unbox() ast.MiniObj {
	return p.MiniObj
}

func (r *runtimeMap) Get(key ast.MiniObj) (ast.MiniObj, bool, error) {
	goKey := toGoValue(key)
	val, ok := r.data[goKey]
	if !ok {
		return nil, false, nil
	}
	mObj, err := toMiniObj(val)
	if err != nil {
		return nil, ok, err
	}
	return &mapValueProxy{MiniObj: mObj, m: r, key: key}, ok, nil
}

func (r *runtimeMap) Set(key ast.MiniObj, val ast.MiniObj) error {
	goKey := toGoValue(key)
	r.data[goKey] = val
	return nil
}

func (r *runtimeMap) Delete(key ast.MiniObj) error {
	goKey := toGoValue(key)
	delete(r.data, goKey)
	return nil
}

func (r *runtimeMap) Keys() []ast.MiniObj {
	keys := make([]ast.MiniObj, 0, len(r.data))
	for k := range r.data {
		mObj, _ := toMiniObj(k)
		if mObj != nil {
			keys = append(keys, mObj)
		}
	}
	return keys
}

// NewRuntimeArray 创建一个运行时数组代理
func NewRuntimeArray(data *[]any, elemType ast.GoMiniType) ast.MiniArray {
	return &runtimeArray{data: data, elemType: elemType}
}

// NewRuntimeMap 创建一个运行时映射代理
func NewRuntimeMap(data map[any]any, keyType, valType ast.GoMiniType) ast.MiniMap {
	return &runtimeMap{data: data, keyType: keyType, valType: valType}
}

func toMiniObj(v any) (ast.MiniObj, error) {
	if v == nil {
		return nil, nil
	}
	if m, ok := v.(ast.MiniObj); ok {
		return m, nil
	}
	// Fallback for value types that implement MiniObj only via pointer
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if m, ok := v.(ast.MiniObj); ok {
			return m, nil
		}
	} else {
		// Value types: MiniString, MiniInt64 etc.
		if s, ok := v.(ast.MiniString); ok {
			return &s, nil
		}
		if i, ok := v.(ast.MiniInt64); ok {
			return &i, nil
		}
		if b, ok := v.(ast.MiniBool); ok {
			return &b, nil
		}
		if f, ok := v.(ast.MiniFloat64); ok {
			return &f, nil
		}
		if u, ok := v.(ast.MiniUint8); ok {
			return &u, nil
		}
	}

	// Wrapper for raw maps and slices
	if rv.Kind() == reflect.Map {
		if data, ok := v.(map[any]any); ok {
			return &runtimeMap{data: data, keyType: "Any", valType: "Any"}, nil
		}
	}
	if rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Slice {
		if data, ok := v.(*[]any); ok {
			return &runtimeArray{data: data, elemType: "Any"}, nil
		}
	}

	// Fallback for primitive types if they are not yet wrapped
	switch val := v.(type) {
	case string:
		s := ast.NewMiniString(val)
		return &s, nil
	case int64:
		i := ast.NewMiniInt64(val)
		return &i, nil
	case float64:
		f := ast.NewMiniFloat64(val)
		return &f, nil
	case bool:
		b := ast.NewMiniBool(val)
		return &b, nil
	case byte:
		u := ast.NewMiniUint8(val)
		return &u, nil
	}
	return nil, fmt.Errorf("value of type %T is not a MiniObj", v)
}

func toGoValue(v any) any {
	if v == nil {
		return nil
	}
	// Unwrap proxy first
	v = UnwrapProxy(v)
	if gv, ok := v.(ast.GoMiniValue); ok {
		return gv.GoValue()
	}
	return v
}

// UnwrapProxy 如果是代理对象则解包，否则返回原对象
func UnwrapProxy(v any) any {
	for {
		if p, ok := v.(interface{ Unbox() ast.MiniObj }); ok {
			v = p.Unbox()
		} else {
			break
		}
	}
	return v
}
