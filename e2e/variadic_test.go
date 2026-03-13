package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

type VariadicTester struct {
	Captured []string
}

func (v *VariadicTester) OPSType() ast.Ident {
	return "VariadicTester"
}

func (v *VariadicTester) Aa(selectors ...*ast.MiniString) error {
	for _, s := range selectors {
		v.Captured = append(v.Captured, s.GoString())
	}
	return nil
}

func (v *VariadicTester) Bb(a *ast.MiniString, b ...*ast.MiniString) error {
	v.Captured = append(v.Captured, "fixed:"+a.GoString())
	for _, s := range b {
		v.Captured = append(v.Captured, "variadic:"+s.GoString())
	}
	return nil
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
