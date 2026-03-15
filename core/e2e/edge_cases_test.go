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

func TestEdgeCases(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	var results []string
	executor.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	// mock function that logs when it is called (useful for short-circuit testing)
	executor.MustAddFunc("IsTrue", func() ast.MiniBool {
		results = append(results, "IsTrue_called")
		return ast.NewMiniBool(true)
	})
	executor.MustAddFunc("IsFalse", func() ast.MiniBool {
		results = append(results, "IsFalse_called")
		return ast.NewMiniBool(false)
	})

	t.Run("short_circuit_and", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				if IsFalse() && IsTrue() {
					push("unreachable")
				}
				push("done")
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		if !assert.NoError(t, err) {
			return
		}
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"IsFalse_called", "done"}, results)
	})

	t.Run("short_circuit_or", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				if IsTrue() || IsFalse() {
					push("reachable")
				}
				push("done")
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		if !assert.NoError(t, err) {
			return
		}
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"IsTrue_called", "reachable", "done"}, results)
	})

	t.Run("unary_operations", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				a := true
				push(!a)
				b := 10
				push(-b)
				push(+b)
				// Bitwise NOT
				c := ^10
				push(c)
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		if !assert.NoError(t, err) {
			return
		}
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"false", "-10", "10", "-11"}, results)
	})

	t.Run("inc_dec_operations", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				a := 10
				a++
				push(a)
				b := 10.5
				b--
				push(b)
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		if !assert.NoError(t, err) {
			return
		}
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"11", "9.5"}, results)
	})

	t.Run("switch_multiple_cases", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				v := 2
				switch v {
				case 1, 2, 3:
					push("1-3")
				default:
					push("other")
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		if !assert.NoError(t, err) {
			return
		}
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"1-3"}, results)
	})

	t.Run("assignment_operations", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				a := 10
				a += 5
				push(a)
				a -= 2
				push(a)
				a *= 2
				push(a)
				a /= 2
				push(a)
				a %= 3
				push(a)
				
				s := "hello"
				s += " world"
				push(s)
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		if !assert.NoError(t, err) {
			return
		}
		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"15", "13", "26", "13", "1", "hello world"}, results)
	})
}
