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
	Fields     map[Ident]OPSType
	Methods    map[Ident]CallFunctionType
	LiteralNew bool
}
type MiniObj interface {
	OPSType() Ident
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
	structName := obj.OPSType()
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

func parseFields(t reflect.Type) map[Ident]OPSType {
	fields := make(map[Ident]OPSType)
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

func parseMiniType(field reflect.Type) (OPSType, bool) {
	if field.Kind() == reflect.Slice || field.Kind() == reflect.Array {
		elemType, b := parseMiniType(field.Elem())
		if !b {
			return "", false
		}
		return CreateArrayType(elemType), true
	}
	isPtr := field.Kind() == reflect.Ptr
	if !isPtr && field.String() == "interface {}" {
		return TypeAny, true
	}
	if isPtr {
		field = field.Elem()
	}

	// 基础类型支持
	switch field.Kind() {
	case reflect.String:
		miniType := OPSType("String")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Bool:
		miniType := OPSType("Bool")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Int:
		miniType := OPSType("Int")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Int8:
		miniType := OPSType("Int8")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Int16:
		miniType := OPSType("Int16")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Int32:
		miniType := OPSType("Int32")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Int64:
		miniType := OPSType("Int64")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Uint:
		miniType := OPSType("Uint")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Uint8:
		miniType := OPSType("Uint8")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Uint16:
		miniType := OPSType("Uint16")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Uint32:
		miniType := OPSType("Uint32")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Uint64:
		miniType := OPSType("Uint64")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Uintptr:
		miniType := OPSType("Uintptr")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Float32:
		miniType := OPSType("Float32")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Float64:
		miniType := OPSType("Float64")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Complex64:
		miniType := OPSType("Complex64")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	case reflect.Complex128:
		miniType := OPSType("Complex128")
		if isPtr {
			miniType = OPSType(fmt.Sprintf("Ptr<%v>", miniType))
		}
		return miniType, true
	default:
	}

	// 基础接口支持
	if field.Kind() == reflect.Interface {
		if field.Implements(errorType) {
			return "Error", true
		}
		if field.String() == "interface {}" {
			return TypeAny, true
		}
	}

	if !isMiniType(reflect.PointerTo(field)) {
		return "", false
	}
	miniType := reflect.New(field).Interface().(MiniObj).OPSType()
	res := OPSType(miniType)
	if isPtr {
		res = res.ToPtr()
	}
	return res, true
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
		if method.Name == "OPSType" {
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
	outTypes := make([]OPSType, 0)
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
	outType := OPSType("Void")
	if len(outTypes) > 0 {
		if len(outTypes) > 1 {
			outType = CreateTupleType(outTypes...)
		} else {
			outType = outTypes[0]
		}
	}
	callFunc, _ := OPSType(fmt.Sprintf("function(%s) %s", strings.Join(inTypes, ","), outType)).ReadCallFunc()
	return &callFunc, true
}
