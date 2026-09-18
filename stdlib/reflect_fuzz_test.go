package stdlib_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func FuzzReflectRuntimeSemantics(f *testing.F) {
	f.Add(`json:"name"`, "json", int64(0))
	f.Add(`note:"line\nquote\"" empty:""`, "note", int64(1))
	f.Add(`broken:"\x"`, "broken", int64(2))

	const root = "minigo.test/reflect-fuzz"
	program := prepareStdlibProgram(f, root, `package main

import (
	"reflect"
	"strconv"
)

type reflectFuzzLeaf struct {
	Value int
}

type reflectFuzzLeft struct {
	reflectFuzzLeaf
}

type reflectFuzzRight struct {
	reflectFuzzLeaf
}

type reflectFuzzRoot struct {
	reflectFuzzLeft
	reflectFuzzRight
	Direct string
}

func Run(tag string, key string, mode int64) (result string) {
	value, found := reflect.StructTag(tag).Lookup(key)
	result = strconv.FormatBool(found) + ":" + value
	length := int(uint64(mode) % 8)
	arrayType := reflect.ArrayOf(length, reflect.TypeOf(int(0)))
	if arrayType != reflect.ArrayOf(length, reflect.TypeOf(int(0))) {
		panic("unstable reflect type identity")
	}
	types := map[reflect.Type]int{arrayType: length}
	if types[arrayType] != length || arrayType.Len() != length || arrayType.Elem().Kind() != reflect.Int {
		panic("invalid structured reflect type metadata")
	}
	field, fieldFound := reflect.TypeOf(reflectFuzzRoot{}).FieldByNameFunc(func(name string) bool {
		return name == key
	})
	if fieldFound {
		result += ":" + field.Name
	} else {
		result += ":-"
	}
	defer func() {
		if recover() != nil {
			result = "panic:" + result
		}
	}()
	typ := reflect.TypeOf((func(int) int)(nil))
	function := reflect.MakeFunc(typ, func(arguments []reflect.Value) []reflect.Value {
		switch mode % 3 {
		case 0:
			return []reflect.Value{reflect.ValueOf(int(arguments[0].Int()) + 1)}
		case 1:
			return []reflect.Value{}
		default:
			return []reflect.Value{reflect.ValueOf("wrong")}
		}
	})
	values := function.Call([]reflect.Value{reflect.ValueOf(41)})
	return result + ":" + strconv.FormatInt(values[0].Int(), 10)
}
`, []compiler.EntryPoint{{Name: "run", ModulePath: root, Function: "Run"}})

	f.Fuzz(func(t *testing.T, tag, key string, mode int64) {
		if len(tag) > 2048 || len(key) > 256 {
			t.Skip()
		}
		arguments := []minigoruntime.HostValue{
			minigoruntime.HostString(tag), minigoruntime.HostString(key), minigoruntime.HostInt("Int64", mode),
		}
		firstValue, firstError := runFuzzString(program, arguments...)
		secondValue, secondError := runFuzzString(program, arguments...)
		if firstValue != secondValue || firstError != secondError {
			t.Fatalf("non-deterministic reflect result: (%q, %q) != (%q, %q)", firstValue, firstError, secondValue, secondError)
		}
	})
}
