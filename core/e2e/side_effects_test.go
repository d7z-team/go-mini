package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestPointerSideEffects(t *testing.T) {
	e := engine.NewMiniExecutor()
	var results []string

	// 注入一个可以修改指针内容的函数
	// 注意：在严格类型下，我们必须使用 *ast.MiniString
	e.MustAddFunc("ModifyString", func(s *ast.MiniString) {
		s.Set("modified-value")
	})

	e.MustAddFunc("ModifyNumber", func(n *ast.MiniInt64) {
		n.Set(int64(999))
	})

	e.MustAddFunc("push", func(v any) {
		if s, ok := v.(*ast.MiniString); ok {
			results = append(results, s.GoString())
		} else if s, ok := v.(string); ok {
			results = append(results, s)
		} else {
			results = append(results, fmt.Sprintf("%v", v))
		}
	})

	t.Run("VariableSideEffect", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
	s := "initial"
	ModifyString(&s)
	
	n := 100
	ModifyNumber(&n)
	
	// 通过打印验证
	push(s)
	push(n.String())
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)

		assert.Equal(t, []string{"modified-value", "999"}, results)
	})

	t.Run("StructFieldSideEffect", func(t *testing.T) {
		results = nil
		e.AddNativeStruct((*SideEffectUser)(nil))
		code := `
package main
func main() {
	u := User{Name: "initial"}
	ModifyString(&u.Name)
	push(u.Name)
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)

		assert.Equal(t, []string{"modified-value"}, results)
	})
}

type SideEffectUser struct {
	Name ast.MiniString
}

func (u *SideEffectUser) GoMiniType() ast.Ident { return "User" }

func TestPointerSideEffectsComprehensive(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.MustAddFunc("ModifyString", func(s *ast.MiniString) {
		s.Set("modified-value")
	})
	e.AddNativeStruct((*SideEffectUser)(nil))

	var results []string
	e.MustAddFunc("push", func(v any) {
		if s, ok := v.(*ast.MiniString); ok {
			results = append(results, s.GoString())
		} else if s, ok := v.(ast.MiniString); ok {
			results = append(results, s.GoString())
		} else if s, ok := v.(string); ok {
			results = append(results, s)
		}
	})

	t.Run("VariableSideEffect", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
	s := "initial"
	ModifyString(&s)
	push(s)
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		if err != nil {
			t.Logf("Execute error: %v", err)
		}
		assert.NoError(t, err)
		assert.Equal(t, []string{"modified-value"}, results)
	})

	t.Run("StructFieldSideEffect", func(t *testing.T) {
		results = nil
		code := `
package main
func main() {
	u := User{Name: "old"}
	ModifyString(&u.Name)
	push(u.Name)
}
`
		prog, err := e.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = prog.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"modified-value"}, results)
	})
}
