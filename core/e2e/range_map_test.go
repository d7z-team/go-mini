package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/utils"
	"gopkg.d7z.net/go-mini/runtimes"
)

func TestRangeMap(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	var results []string
	executor.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	t.Run("range_map_string_keys", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				m := map[string]int64{"a": 1, "b": 2}
				for k, v := range m {
					push(k)
					push(v)
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)

		assert.Contains(t, results, "a")
		assert.Contains(t, results, "1")
		assert.Contains(t, results, "b")
		assert.Contains(t, results, "2")
		assert.Len(t, results, 4)
	})

	t.Run("range_array_consistency", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				arr := []int64{10, 20}
				for i, v := range arr {
					push(i)
					push(v)
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "10", "1", "20"}, results)
	})

	t.Run("range_map_int64_keys", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				m := map[int64]string{10: "val1", 20: "val2"}
				for k, v := range m {
					push(k)
					push(v)
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)

		assert.Contains(t, results, "10")
		assert.Contains(t, results, "val1")
		assert.Contains(t, results, "20")
		assert.Contains(t, results, "val2")
		assert.Len(t, results, 4)
	})

	t.Run("range_map_bool_keys", func(t *testing.T) {
		results = nil
		code := `
			func main() {
				m := map[bool]string{true: "yes", false: "no"}
				for k, v := range m {
					push(k)
					push(v)
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)

		assert.Contains(t, results, "true")
		assert.Contains(t, results, "yes")
		assert.Contains(t, results, "false")
		assert.Contains(t, results, "no")
		assert.Len(t, results, 4)
	})

	t.Run("range_map_struct_ptr_keys", func(t *testing.T) {
		results = nil
		// 这里由于 Go 代码中 map 的 key 必须可比较，指针是没问题的
		code := `
			type Point struct { X int64 }
			func main() {
				p1 := &Point{X: 1}
				m := map[*Point]string{p1: "first"}
				for k, v := range m {
					push(k.X)
					push(v)
				}
			}
		`
		rt, err := executor.NewRuntimeByGoCode(code)
		assert.NoError(t, err)

		err = rt.Execute(context.Background())
		assert.NoError(t, err)

		assert.Contains(t, results, "1")
		assert.Contains(t, results, "first")
		assert.Len(t, results, 2)
	})
}
