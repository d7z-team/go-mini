package runtimes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOS(t *testing.T) {
	t.Run("getwd", func(t *testing.T) {
		result := runTest(t, `
			import "os"
			func main() {
				wd, _ := os.Getwd()
				push(wd.Len() > 0)
			}
		`)
		assert.Equal(t, []string{"true"}, result)
	})

	t.Run("syscall_helpers", func(t *testing.T) {
		result := runTest(t, `
			func main() {
				r := RandomInt(1, 10)
				push(r >= 1)
				push(r <= 10)
				
				enc := Base64Enc("hello")
				push(Base64Dec(enc))
			}
		`)
		assert.Equal(t, []string{"true", "true", "hello"}, result)
	})
}
