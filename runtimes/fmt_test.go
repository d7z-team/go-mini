package runtimes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFmt(t *testing.T) {
	t.Run("sprintf", func(t *testing.T) {
		result := runTest(t, `
			import "fmt"
			func main() {
				s := fmt.Sprintf("hello %s, score: %d", "mini", 100)
				push(s)
			}
		`)
		assert.Equal(t, []string{"hello mini, score: 100"}, result)
	})
}
