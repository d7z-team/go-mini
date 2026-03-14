package utils

import (
	"context"
	"fmt"
	"reflect"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestGoExpr(goCode string) ([]string, error) {
	return TestGoCode(`
func main(){
` + goCode + `
}
`)
}

func FormatValue(data any) string {
	if data == nil {
		return "nil"
	}

	rv := reflect.ValueOf(data)

	check := func(v reflect.Value) (string, bool) {
		if !v.IsValid() {
			return "", false
		}
		i := v.Interface()
		if gv, ok := i.(interface{ GoString() string }); ok {
			return gv.GoString(), true
		}
		if gv, ok := i.(interface{ GoValue() any }); ok {
			return fmt.Sprintf("%v", gv.GoValue()), true
		}
		return "", false
	}

	curr := rv
	for {
		if s, ok := check(curr); ok {
			return s
		}
		// If it's a value, try its pointer
		if curr.CanAddr() {
			if s, ok := check(curr.Addr()); ok {
				return s
			}
		}

		if curr.Kind() == reflect.Ptr && !curr.IsNil() {
			curr = curr.Elem()
		} else {
			break
		}
	}

	return fmt.Sprintf("%v", curr.Interface())
}

func TestGoCode(goCode string) ([]string, error) {
	result := make([]string, 0)
	miniExecutor := engine.NewMiniExecutor()
	for _, stdlibStruct := range ast.StdlibStructs {
		miniExecutor.AddNativeStruct(stdlibStruct)
	}

	miniExecutor.MustAddFunc("push_num", func(n *ast.MiniInt64) {
		if n == nil {
			result = append(result, "nil")
			return
		}
		result = append(result, fmt.Sprintf("%v", n.GoValue()))
	})

	miniExecutor.MustAddFunc("println", func(data any) {
		switch item := data.(type) {
		case ast.MiniOsString:
			//nolint:forbidigo // Test utility output
			fmt.Println(item.GoString())
		default:
			//nolint:forbidigo // Test utility output
			fmt.Printf("(%T)%v\n", data, data)
		}
	})
	miniExecutor.MustAddFunc("print", func(data any) {
		switch item := data.(type) {
		case ast.MiniOsString:
			//nolint:forbidigo // Test utility output
			fmt.Print(item.GoString())
		default:
			//nolint:forbidigo // Test utility output
			fmt.Printf("%v", data)
		}
	})
	miniExecutor.MustAddFunc("push", func(data any) {
		result = append(result, FormatValue(data))
	})
	code, err := miniExecutor.NewRuntimeByGoCode(goCode)
	if err != nil {
		return nil, err
	}
	return result, code.Execute(context.TODO())
}
