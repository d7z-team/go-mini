package e2e_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/runtimes"
)

func runCastTest(t *testing.T, code string) []string {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	var results []string
	executor.MustAddFunc("push", func(v any) {
		// 检查是否为指针
		rv := reflect.ValueOf(v)
		val := v
		if rv.Kind() == reflect.Ptr && !rv.IsNil() {
			val = rv.Elem().Interface()
		}

		// 统一通过 GoMiniValue 接口提取原始值
		if gv, ok := val.(ast.GoMiniValue); ok {
			val = gv.GoValue()
		} else if gv, ok := v.(ast.GoMiniValue); ok {
			// 针对某些实现可能挂在指针上的情况
			val = gv.GoValue()
		}

		results = append(results, fmt.Sprintf("%v", val))
	})

	rt, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to parse/validate: %v", err)
	}

	err = rt.Execute(context.Background())
	if err != nil {
		t.Fatalf("failed to execute: %v", err)
	}

	return results
}

func TestTypeCast(t *testing.T) {
	t.Run("basic_literal_casts", func(t *testing.T) {
		result := runCastTest(t, `
			func main() {
				i := int64(12)
				f := float64(3.14)
				s := string("hello")
				push(i)
				push(f)
				push(s)
			}
		`)
		assert.Equal(t, []string{"12", "3.14", "hello"}, result)
	})

	t.Run("variable_casts", func(t *testing.T) {
		result := runCastTest(t, `
			func main() {
				i := 100
				i2 := int64(i)
				f := float64(i2)
				s := string(i2) // 这里的转换逻辑依赖于 ast.MiniString.New(string)
				push(i2)
				push(f)
				push(s)
			}
		`)
		// 注意：Int64 -> String 在当前实现下是通过 fmt.Sprintf("%v", val) 转换的
		assert.Equal(t, []string{"100", "100", "100"}, result)
	})

	t.Run("implicit_numeric_conversion_in_binary_ops", func(t *testing.T) {
		executor := engine.NewMiniExecutor()
		runtimes.InitAll(executor)

		var results []string
		executor.MustAddFunc("push", func(v any) {
			// 统一通过反射和 GoMiniValue 提取原始值
			rv := reflect.ValueOf(v)
			val := v
			if rv.Kind() == reflect.Ptr && !rv.IsNil() {
				val = rv.Elem().Interface()
			}
			if gv, ok := val.(ast.GoMiniValue); ok {
				val = gv.GoValue()
			} else if gv, ok := v.(ast.GoMiniValue); ok {
				val = gv.GoValue()
			}
			results = append(results, fmt.Sprintf("%v", val))
		})

		// 定义返回 MiniInt 的函数
		executor.MustAddFunc("Size", func() ast.MiniInt {
			return ast.NewMiniInt(20)
		})

		code := `
			func main() {
				if Size() > 10 { // Int(20) > Int64(10)
					push("greater")
				}

				// 显式混合运算
				a := int8(5)
				b := 10 // Int64
				if a < b {
					push("less")
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"greater", "less"}, results)
	})
}
