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
		if data == nil {
			result = append(result, "nil")
			return
		}

		// Try to resolve GoString from pointers or values
		rv := reflect.ValueOf(data)
		for {
			if item, ok := rv.Interface().(ast.MiniOsString); ok {
				result = append(result, item.GoString())
				return
			}
			if item, ok := rv.Interface().(ast.GoMiniValue); ok {
				result = append(result, fmt.Sprintf("%v", item.GoValue()))
				return
			}
			if rv.Kind() == reflect.Ptr && !rv.IsNil() {
				rv = rv.Elem()
				continue
			}
			// Try taking address of value
			if rv.Kind() != reflect.Ptr && rv.CanAddr() {
				if item, ok := rv.Addr().Interface().(ast.MiniOsString); ok {
					result = append(result, item.GoString())
					return
				}
				if item, ok := rv.Addr().Interface().(ast.GoMiniValue); ok {
					result = append(result, fmt.Sprintf("%v", item.GoValue()))
					return
				}
			} else if rv.Kind() != reflect.Ptr {
				// If not addressable, create a new pointer
				tmp := reflect.New(rv.Type())
				tmp.Elem().Set(rv)
				if item, ok := tmp.Interface().(ast.MiniOsString); ok {
					result = append(result, item.GoString())
					return
				}
				if item, ok := tmp.Interface().(ast.GoMiniValue); ok {
					result = append(result, fmt.Sprintf("%v", item.GoValue()))
					return
				}
			}
			break
		}

		result = append(result, fmt.Sprintf("%v", data))
	})
	code, err := miniExecutor.NewRuntimeByGoCode(goCode)
	if err != nil {
		return nil, err
	}
	return result, code.Execute(context.TODO())
}
