package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestProxySideEffects(t *testing.T) {
	e := engine.NewMiniExecutor()

	e.MustAddFunc("ModifyArray", func(arr ast.MiniArray) {
		// arr[0] = "modified"
		ms := ast.NewMiniString("modified")
		_ = arr.Set(0, &ms)
	})

	e.MustAddFunc("ModifyMap", func(m ast.MiniMap) {
		// m["key"] = "modified"
		mk := ast.NewMiniString("key")
		mv := ast.NewMiniString("modified")
		_ = m.Set(&mk, &mv)
	})

	var results []string
	e.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	t.Run("ArraySideEffect", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
	arr := []string{"initial"}
	ModifyArray(arr)
	push(arr[0])
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"modified"}, results)
	})

	t.Run("MapSideEffect", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
	m := map[string]string{"key": "initial"}
	ModifyMap(m)
	push(m["key"])
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"modified"}, results)
	})
}
