package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestArray(t *testing.T) {
	t.Run("literal_and_access", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			arr := []int{1, 2, 3}
			push(arr[0])
			push(arr[1])
			push(arr[2])
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2", "3"}, result)
	})

	t.Run("modification", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			arr := []int{1, 2, 3}
			arr[0] = 10
			push(arr[0])
			arr.set(1, 20)
			push(arr[1])
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"10", "20"}, result)
	})

	t.Run("methods", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			arr := []int{1}
			arr.push(2)
			push(arr.length())
			push(arr.get(1))
			arr.remove(0)
			push(arr.length())
			push(arr.get(0))
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"2", "2", "1", "2"}, result)
	})

	t.Run("loop", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			arr := []int{1, 2, 3}
			push(arr.length())
			for i := 0; i < arr.length(); i++ {
				push(arr[i])
			}
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"3", "1", "2", "3"}, result)
	})
}

func TestMap(t *testing.T) {
	t.Run("literal_and_access", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			m := map[string]int{"a": 1, "b": 2}
			push(m["a"])
			push(m.get("b"))
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "2"}, result)
	})

	t.Run("modification", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			m := map[string]int{"a": 1}
			m["a"] = 10
			push(m["a"])
			m["c"] = 3
			push(m["c"])
			m.put("d", 4)
			push(m["d"])
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"10", "3", "4"}, result)
	})

	t.Run("methods", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			m := map[string]int{"a": 1}
			push(m.size())
			push(m.contains("a"))
			push(m.contains("z"))
			m.remove("a")
			push(m.size())
			push(m.contains("a"))
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "true", "false", "0", "false"}, result)
	})

	t.Run("keys_values", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			m := map[string]int{"a": 1, "b": 2}
			keys := m.keys()
			values := m.values()
			push(keys.length())
			push(values.length())
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"2", "2"}, result)
	})
}
