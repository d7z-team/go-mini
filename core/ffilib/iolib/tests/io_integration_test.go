package iolib_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestReadAllBytes(t *testing.T) {
	testutil.Run(t, `
package main
import "io"

func main() {
	data, err := io.ReadAll([]byte("bytes"))
	if err != nil || string(data) != "bytes" {
		panic("io.ReadAll failed")
	}
}
`)
}

func TestWriteStringAndSeek(t *testing.T) {
	path := filepath.Join(t.TempDir(), "io.txt")
	testutil.Run(t, fmt.Sprintf(`
package main
import "io"
import "os"

func main() {
	f, err := os.Create(%q)
	if err != nil { panic(err) }
	if _, err = io.WriteString(f, "hello"); err != nil { panic(err) }
	if _, err = f.Seek(0, io.SeekStart); err != nil { panic(err) }
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "hello" { panic("readback failed") }
	if err = f.Close(); err != nil { panic(err) }
}
`, path))
}
