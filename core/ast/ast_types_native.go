package ast

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// MiniOsString 接口专门用于表示那些可以与操作系统（宿主）
// 直接交互的字符串类型。主要为文件系统、路径等标准库操作提供便利。
type MiniOsString interface {
	MiniObj
	GoString() string
	String() MiniString
}

// GoMiniValue 是所有 GoMini 基础类型包装器（如 MiniInt, MiniString）
// 都会实现的接口。它允许引擎在与 Native Go 代码交互时，自动将 VM 里的
// 包装对象“拆箱”回原生的 Go 值（如将 *MiniInt64 转换为 int64），
// 极大地简化了 Native API 的参数传递负担。
type GoMiniValue interface {
	GoValue() any
}

// MiniObj 是 GoMini 引擎中所有自定义对象的根接口。
// 为了在脚本中保证类型安全和沙箱隔离，任何希望暴露给脚本层操作的
// 原生 Go 结构体（除基础内置类型、Array、Map 外）都必须实现此接口。
// 它声明了对象在 GoMini 环境中的“身份”（即类型标识）。
type MiniObj interface {
	GoMiniType() Ident
}

// MinObjValue 是一个标记接口，用于声明该对象应该被视为严格的“值类型”。
// 暂时作为未来值语义扩展的保留接口。
type MinObjValue interface {
	GoMiniTypeValue()
}

// MiniClone 接口用于为对象提供深拷贝能力。
// 在执行器进行“值传递”（如赋值或函数传参）时，如果非指针对象实现了此接口，
// 引擎会主动调用 Clone() 来拷贝数据，避免浅拷贝引发的内存逃逸或引用副作用。
// 这是保证 GoMini 中“值语义”（Value Semantics）的核心机制之一。
type MiniClone interface {
	MiniObj
	Clone() MiniObj
}

// MiniObjLiteral 接口允许对象从一个字符串字面量中直接构造自身。
// 实现该接口的类型在注册时，引擎会自动为其生成一个 '__obj__new__Type' 方法，
// 支持使用诸如 `Type("value")` 的语法在脚本中进行初始化。
type MiniObjLiteral interface {
	MiniObj
	New(string) (MiniObj, error)
}

type NativeStruct struct {
	Type       reflect.Type
	StructName Ident
	Fields     map[Ident]GoMiniType
	Methods    map[Ident]CallFunctionType
	LiteralNew bool
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
			res, ok = "Error", true
		} else if field.NumMethod() == 0 {
			res, ok = TypeAny, true
		}
	default:
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
