package ast

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

type NativeStruct struct {
	Type       reflect.Type
	StructName Ident
	Fields     map[Ident]GoMiniType
	Methods    map[Ident]CallFunctionType
	LiteralNew bool
}
type MiniObj interface {
	GoMiniType() Ident
}

type MinObjValue interface {
	GoMiniTypeValue()
}

type MiniClone interface {
	MiniObj
	Clone() MiniObj
}

type MiniObjLiteral interface {
	MiniObj
	New(string) (MiniObj, error)
}

func ParseNative(t reflect.Type) (*NativeStruct, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, errors.New("native struct required")
	}
	inst := reflect.New(t).Interface()
	obj := inst.(MiniObj)
	structName := obj.GoMiniType()
	_, ok := obj.(MiniObjLiteral)
	return &NativeStruct{
		Type:       t,
		StructName: structName,
		Fields:     parseFields(t),
		Methods:    parseMethods(t),
		LiteralNew: ok,
	}, nil
}

func isMiniType(t reflect.Type) bool {
	moverType := reflect.TypeOf((*MiniObj)(nil)).Elem()
	return t.Implements(moverType)
}

func parseFields(t reflect.Type) map[Ident]GoMiniType {
	fields := make(map[Ident]GoMiniType)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		o, b := parseMiniType(field.Type)
		if !b {
			continue
		}
		fieldIdent := Ident(field.Name)
		fields[fieldIdent] = o
	}
	return fields
}

func parseMiniType(field reflect.Type) (GoMiniType, bool) {
	isPtr := field.Kind() == reflect.Ptr
	if isPtr {
		field = field.Elem()
	}

	var res GoMiniType
	var ok bool

	// 基础类型支持
	switch field.Kind() {
	case reflect.Slice, reflect.Array:
		elemType, b := parseMiniType(field.Elem())
		if b {
			res, ok = CreateArrayType(elemType), true
		}
	case reflect.Map:
		keyType, b1 := parseMiniType(field.Key())
		valType, b2 := parseMiniType(field.Elem())
		if b1 && b2 {
			res, ok = CreateMapType(keyType, valType), true
		}
	case reflect.Interface:
		if field.Implements(errorType) {
			res, ok = GoMiniType("Error"), true
		} else if field.NumMethod() == 0 {
			res, ok = TypeAny, true
		}
	}

	if !ok {
		// 非基础类型必须通过指针实现 MiniObj
		if isMiniType(reflect.PointerTo(field)) {
			miniType := reflect.New(field).Interface().(MiniObj).GoMiniType()
			res, ok = GoMiniType(miniType), true
		}
	}

	if ok {
		if isPtr {
			res = res.ToPtr()
		}
		return res, true
	}

	return "", false
}

var (
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

// PackageStructWrapper 用于包装带包名的原生结构体
type PackageStructWrapper struct {
	Pkg  string
	Name string
	Stru any
}

func parseMethods(t reflect.Type) map[Ident]CallFunctionType {
	if t.Kind() != reflect.Ptr {
		t = reflect.PointerTo(t)
	}
	members := make(map[Ident]CallFunctionType)
	// 获取所有方法
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		if method.Name == "GoMiniType" {
			continue
		}
		parseMethod, b := ParseMethod(method.Type)
		if !b {
			continue
		}

		doc := GetFuncDoc(method.Func.Interface())
		parseMethod.Doc = doc

		members[Ident(method.Name)] = *parseMethod
	}
	return members
}

func ParseMethod(methodType reflect.Type) (*CallFunctionType, bool) {
	numIn := methodType.NumIn()
	numOut := methodType.NumOut()

	inTypes := make([]string, 0)
	outTypes := make([]GoMiniType, 0)
	for i := 0; i < numIn; i++ {
		inType := methodType.In(i)
		// input 允许第一位为 context
		if i == 0 && inType == contextType {
			continue
		}
		miniType, b := parseMiniType(inType)
		if !b {
			return nil, false
		}
		typeStr := string(miniType)
		if methodType.IsVariadic() && i == numIn-1 {
			typeStr = "..." + typeStr
		}
		inTypes = append(inTypes, typeStr)
	}
	for i := 0; i < numOut; i++ {
		outType := methodType.Out(i)
		// output允许最后一位为 error
		if i == numOut-1 && outType.Implements(errorType) {
			continue
		}
		miniType, b := parseMiniType(outType)
		if !b {
			return nil, false
		}
		outTypes = append(outTypes, miniType)
	}
	outType := GoMiniType("Void")
	if len(outTypes) > 0 {
		if len(outTypes) > 1 {
			outType = CreateTupleType(outTypes...)
		} else {
			outType = outTypes[0]
		}
	}
	callFunc, _ := GoMiniType(fmt.Sprintf("function(%s) %s", strings.Join(inTypes, ","), outType)).ReadCallFunc()
	return &callFunc, true
}
