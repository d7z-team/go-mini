package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/utils"
	"gopkg.d7z.net/go-mini/runtimes"
)

func TestNativeMapReturn(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	// 注册返回 MiniMap 的函数
	executor.MustAddFunc("List", func() ast.MiniMap {
		res := make(map[any]any)
		k1 := ast.NewMiniString("k1")
		v1 := ast.NewMiniString("v1")
		res[k1.GoString()] = &v1
		return runtime.NewRuntimeMap(res, "String", "String")
	})

	var results []string
	executor.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	t.Run("call_native_map_return", func(t *testing.T) {
		code := `
			func main() {
				m := List()
				// push(m) // 这里的 push 会处理 map
				push(m["k1"])
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"v1"}, results)
	})
}
