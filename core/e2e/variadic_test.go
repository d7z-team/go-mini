package e2e_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

type VariadicTester struct {
	Captured []string
}

func (v *VariadicTester) GoMiniType() ast.Ident {
	return "VariadicTester"
}

func (v *VariadicTester) Aa(selectors ast.MiniArray) error {
	for i := 0; i < selectors.Len(); i++ {
		item, _ := selectors.Get(i)
		// 自动解包和处理指针
		val := unwrap(item)
		if s, ok := val.(*ast.MiniString); ok {
			v.Captured = append(v.Captured, s.GoString())
		}
	}
	return nil
}

func (v *VariadicTester) Bb(a *ast.MiniString, b ast.MiniArray) error {
	v.Captured = append(v.Captured, "fixed:"+a.GoString())
	for i := 0; i < b.Len(); i++ {
		item, _ := b.Get(i)
		// 自动解包和处理指针
		val := unwrap(item)
		if s, ok := val.(*ast.MiniString); ok {
			v.Captured = append(v.Captured, "variadic:"+s.GoString())
		}
	}
	return nil
}

func unwrap(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for {
		if rv.Kind() == reflect.Ptr && !rv.IsNil() {
			// 检查是否为代理对象 (通过反射检查 Unbox 方法，避免循环依赖)
			if unbox := rv.MethodByName("Unbox"); unbox.IsValid() {
				res := unbox.Call(nil)
				rv = res[0]
				continue
			}
			rv = rv.Elem()
			continue
		}
		// 检查非指针形式的代理
		if unbox := rv.MethodByName("Unbox"); unbox.IsValid() {
			res := unbox.Call(nil)
			rv = res[0]
			continue
		}
		break
	}
	return rv.Interface()
}

func TestVariadic(t *testing.T) {
	tester := &VariadicTester{}
	exe := engine.NewMiniExecutor()
	exe.AddNativeStruct(tester)

	t.Run("single variadic", func(t *testing.T) {
		tester.Captured = nil
		exe.AddFunc("GetTester", func() *VariadicTester { return tester })
		program, err := exe.NewRuntimeByGoCode(`
func main() {
	v := GetTester()
	v.Aa("1", "2", "asasas")
}
`)
		assert.NoError(t, err)
		err = program.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2", "asasas"}, tester.Captured)
	})

	t.Run("mixed variadic", func(t *testing.T) {
		tester.Captured = nil
		program, err := exe.NewRuntimeByGoCode(`
func main() {
	v := GetTester()
	v.Bb("fixed", "v1", "v2")
}
`)
		assert.NoError(t, err)
		err = program.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"fixed:fixed", "variadic:v1", "variadic:v2"}, tester.Captured)
	})

	t.Run("empty variadic", func(t *testing.T) {
		tester.Captured = nil
		program, err := exe.NewRuntimeByGoCode(`
func main() {
	v := GetTester()
	v.Aa()
}
`)
		assert.NoError(t, err)
		err = program.Execute(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, tester.Captured)
	})
}
