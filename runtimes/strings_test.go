package runtimes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStrings(t *testing.T) {
	t.Run("basic_operations", func(t *testing.T) {
		result := runTest(t, `
			import "strings"
			func main() {
				s := "hello world"
				push(strings.Contains(s, "world"))
				push(strings.ToUpper(s))
				push(strings.ReplaceAll(s, "world", "mini"))
			}
		`)
		assert.Equal(t, []string{"true", "HELLO WORLD", "hello mini"}, result)
	})

	t.Run("split", func(t *testing.T) {
		result := runTest(t, `
			import "strings"
			func main() {
				parts := strings.Split("a,b,c", ",")
				push(parts.length())
				push(parts.get(0))
			}
		`)
		assert.Equal(t, []string{"3", "a"}, result)
	})
}
