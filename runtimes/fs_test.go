package runtimes_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFS(t *testing.T) {
	t.Run("memory_fs", func(t *testing.T) {
		result := runTest(t, `
			import "fs"
			func main() {
				mem := fs.Memory()
				mem.MkdirAll("/test")
				mem.WriteString("/test/f.txt", "hello")
				push(mem.Exists("/test/f.txt"))
				push(mem.ReadString("/test/f.txt"))
			}
		`)
		assert.Equal(t, []string{"true", "hello"}, result)
	})

	t.Run("io_buffer", func(t *testing.T) {
		result := runTest(t, `
			import "io"
			func main() {
				buf := io.NewBuffer()
				buf.MiniWriteString("hello")
				push(buf.String())
			}
		`)
		assert.Equal(t, []string{"hello"}, result)
	})
}
