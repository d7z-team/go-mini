package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestProxyAdvanced(t *testing.T) {
	e := engine.NewMiniExecutor()

	// 1. MiniStruct 副作用验证
	e.MustAddFunc("ModifyStruct", func(s ast.MiniStruct) error {
		val, err := s.GetField("Name")
		if err != nil {
			return err
		}
		val = runtime.UnwrapProxy(val).(ast.MiniObj)
		if ms, ok := val.(*ast.MiniString); ok {
			newVal := ast.NewMiniString(ms.GoString() + " - modified by native")
			err = s.SetField("Name", &newVal)
			if err != nil {
				return err
			}
			age := ast.NewMiniInt64(30)
			return s.SetField("Age", &age)
		}
		return fmt.Errorf("field Name is not a string, got %T", val)
	})

	// 2. 嵌套容器验证 (Array<Map>)
	e.MustAddFunc("ModifyNested", func(arr ast.MiniArray) error {
		// 修改 arr[0] 这个 Map 的值
		item, _ := arr.Get(0)
		item = runtime.UnwrapProxy(item).(ast.MiniObj)
		if m, ok := item.(ast.MiniMap); ok {
			k := ast.NewMiniString("status")
			v := ast.NewMiniString("ok")
			return m.Set(&k, &v)
		}
		return fmt.Errorf("arr[0] is not a map, got %T", item)
	})

	// 3. Map 删除和 Array 追加验证
	e.MustAddFunc("ModifyCollections", func(arr ast.MiniArray, m ast.MiniMap) error {
		// 追加元素到数组
		val := ast.NewMiniString("appended")
		if err := arr.Append(&val); err != nil {
			return err
		}

		// 删除 Map 中的键
		k := ast.NewMiniString("to_delete")
		if err := m.Delete(&k); err != nil {
			return err
		}

		return nil
	})

	var results []string
	e.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	t.Run("StructSideEffects", func(t *testing.T) {
		results = nil
		code := `
package main
type User struct {
	Name String
	Age  Int64
}
func main() {
	u := User{Name: "Alice", Age: 18}
	ModifyStruct(u)
	push(u.Name)
	push(u.Age.String())
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"Alice - modified by native", "30"}, results)
	})

	t.Run("NestedContainerSideEffects", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
		m := map[string]string{"status": "pending"}
		arr := []map[string]string{m}
		ModifyNested(arr)
		push(m["status"])
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"ok"}, results)
	})

	t.Run("CollectionModifications", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
	arr := []string{"first"}
	m := map[string]string{"keep": "yes", "to_delete": "no"}
	
	ModifyCollections(arr, m)
	
	push(arr.length())
	push(arr[1])
	push(m.size())
	push(m.contains("to_delete"))
	push(m["keep"])
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"2", "appended", "1", "false", "yes"}, results)
	})
}
