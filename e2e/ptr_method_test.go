package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestPointerMethods(t *testing.T) {
	t.Run("struct_value_receiver_on_ptr", func(t *testing.T) {
		result, err := utils.TestGoCode(`
			type User struct { name String }
			func (u User) GetName() String { return u.name }
			func main() {
				u := User{name: "Alice"}
				p := &u
				push(p.GetName())
			}
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"Alice"}, result)
	})

	t.Run("array_ptr_indexing_and_methods", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			arr := []Int64{10, 20}
			p := &arr
			push(p[0])
			push(p.length())
			p.push(30)
			push(p[2])
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"10", "2", "30"}, result)
	})

	t.Run("map_ptr_indexing_and_methods", func(t *testing.T) {
		result, err := utils.TestGoExpr(`
			m := map[String]Int64{"a": 1}
			p := &m
			push(p["a"])
			push(p.size())
			p.put("b", 2)
			push(p["b"])
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "1", "2"}, result)
	})

	t.Run("custom_method_on_array_cache", func(t *testing.T) {
		// 验证 GetStruct 缓存是否生效，确保自定义方法在多次获取 struct 时不丢失
		result, err := utils.TestGoCode(`
			func (a []Int64) First() Int64 {
				return a[0]
			}
			func main() {
				arr := []Int64{100, 200}
				push(arr.First())
				
				p := &arr
				push(p.First()) // 结合了指针解引用和自定义方法缓存
			}
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"100", "100"}, result)
	})
}
