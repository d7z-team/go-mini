package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestBugDiscovery(t *testing.T) {
	t.Run("struct_field_assignment_via_pointer", func(t *testing.T) {
		result, err := utils.TestGoCode(`
			type Point struct { X Int64 }
			func main() {
				p := Point{X: 1}
				ptr := &p
				ptr.X = 100
				push(p.X)
			}
		`)
		// 如果 ffigo 把 ptr.X 拍扁成变量，那 p.X 肯定还是 1
		assert.NoError(t, err)
		assert.Equal(t, []string{"100"}, result)
	})
}
