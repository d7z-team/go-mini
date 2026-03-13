package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestByte(t *testing.T) {
	t.Run("byte-literal", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
			var b Byte = 65
			push(b)
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"65"}, data)
	})

	t.Run("byte-array", func(t *testing.T) {
		data, err := utils.TestGoCode(`
			func main() {
				arr := []Byte{72, 105, 33}
				push(arr[0])
				push(arr[1])
				push(arr[2])
			}
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"72", "105", "33"}, data)
	})

	t.Run("byte-compare", func(t *testing.T) {
		data, err := utils.TestGoExpr(`
			var b1 Byte = 10
			var b2 Byte = 10
			var b3 Byte = 20
			push(b1 == b2)
			push(b1 != b3)
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"true", "true"}, data)
	})

	t.Run("byte-slice-mini", func(t *testing.T) {
		data, err := utils.TestGoCode(`
			func main() {
				arr := []Byte{1, 2, 3}
				push(arr[0])
				
				arr[0] = 10
				push(arr[0])
			}
		`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"1", "10"}, data)
	})
}
