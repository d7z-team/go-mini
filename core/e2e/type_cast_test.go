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

	t.Run("different_int_types", func(t *testing.T) {
		// 验证 Go 风格的小写类型名是否通过 ffigo 正确转换
		result := runCastTest(t, `
			func main() {
				a := int8(1)
				b := int16(2)
				c := int32(3)
				d := uint8(4)
				push(a)
				push(b)
				push(c)
				push(d)
			}
		`)
		// ffigo 会将它们映射为 Int64 或 Uint8
		assert.Equal(t, []string{"1", "2", "3", "4"}, result)
	})
}
