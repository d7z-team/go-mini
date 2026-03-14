package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/utils"
	"gopkg.d7z.net/go-mini/runtimes"
)

func TestMathOperations(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	var results []string
	executor.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	executor.MustAddFunc("GetComplex128", func() ast.MiniComplex128 { return ast.NewMiniComplex128(complex(1, 2)) })
	executor.MustAddFunc("GetComplex64", func() ast.MiniComplex64 { return ast.NewMiniComplex64(complex(3, 4)) })
	executor.MustAddFunc("GetFloat32", func() ast.MiniFloat32 { return ast.NewMiniFloat32(5.5) })

	t.Run("complex_operations", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				c1 := GetComplex128()
				c2 := GetComplex128()
				push(c1 == c2)
				push(c1 != c2)
				push(c1 + c2)
				push(c1 - c2)
				push(c1 * c2)
				push(c1 / c2)

				c3 := GetComplex64()
				c4 := GetComplex64()
				push(c3 == c4)
				push(c3 != c4)
				push(c3 + c4)
				push(c3 - c4)
				push(c3 * c4)
				push(c3 / c4)
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{
			"true", "false", "(2+4i)", "(0+0i)", "(-3+4i)", "(1+0i)",
			"true", "false", "(6+8i)", "(0+0i)", "(-7+24i)", "(1+0i)",
		}, results)
	})

	t.Run("float32_operations", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				f1 := GetFloat32()
				f2 := GetFloat32()
				push(f1 == f2)
				push(f1 != f2)
				push(f1 < f2)
				push(f1 <= f2)
				push(f1 > f2)
				push(f1 >= f2)
				push(f1 + f2)
				push(f1 - f2)
				push(f1 * f2)
				push(f1 / f2)
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{
			"true", "false", "false", "true", "false", "true",
			"11", "0", "30.25", "1",
		}, results)
	})
}
