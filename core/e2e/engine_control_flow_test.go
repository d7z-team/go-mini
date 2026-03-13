package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestControlFlowAdvanced(t *testing.T) {
	t.Run("NestedLominiBreak", func(t *testing.T) {
		code := `
package main
func main() {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i == 1 && j == 1 {
				break
			}
			push_num(i * 10 + j)
		}
	}
}
`
		results, err := utils.TestGoCode(code)
		assert.NoError(t, err)
		// Expected: 0, 1, 2, 10, 20, 21, 22
		assert.Equal(t, []string{"0", "1", "2", "10", "20", "21", "22"}, results)
	})

	t.Run("ComplexIfElse", func(t *testing.T) {
		code := `
package main
func main() {
	a := 10
	if a < 5 {
		push("small")
	} else if a < 15 {
		if a == 10 {
			push("exactly ten")
		} else {
			push("medium")
		}
	} else {
		push("large")
	}
}
`
		results, err := utils.TestGoCode(code)
		assert.NoError(t, err)
		assert.Equal(t, []string{"exactly ten"}, results)
	})
}

func TestPanicHandling(t *testing.T) {
	t.Run("ScriptPanic", func(t *testing.T) {
		code := `
package main
func main() {
	Panic("something went wrong")
}
`
		_, err := utils.TestGoCode(code)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "something went wrong")
	})
}
