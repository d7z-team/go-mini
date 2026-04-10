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

func TestFileReadCopyBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "read.txt")
	testutil.Run(t, fmt.Sprintf(`
package main
import "os"

func main() {
	f, err := os.Create(%q)
	if err != nil { panic(err) }
	if _, err = f.Write([]byte("hello world")); err != nil { panic(err) }
	if _, err = f.Seek(0, 0); err != nil { panic(err) }

	buf := []byte(".....")
	n, err := f.Read(buf)
	if err != nil { panic(err) }
	if n != 5 { panic("read length mismatch") }
	if string(buf) != "hello" { panic("read copy-back mismatch") }
	if err = f.Close(); err != nil { panic(err) }
}
`, path))
}

func TestFileReadAtCopyBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readat.txt")
	testutil.Run(t, fmt.Sprintf(`
package main
import "os"

func main() {
	f, err := os.Create(%q)
	if err != nil { panic(err) }
	if _, err = f.Write([]byte("hello world")); err != nil { panic(err) }

	buf := []byte(".....")
	n, err := f.ReadAt(buf, 6)
	if err != nil { panic(err) }
	if n != 5 { panic("readAt length mismatch") }
	if string(buf) != "world" { panic("readAt copy-back mismatch") }
	if err = f.Close(); err != nil { panic(err) }
}
`, path))
}

func TestIOReaderInterface(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reader.txt")
	testutil.Run(t, fmt.Sprintf(`
package main
import "io"
import "os"

func main() {
	f, err := os.Create(%q)
	if err != nil { panic(err) }
	if _, err = f.Write([]byte("hello world")); err != nil { panic(err) }
	if _, err = f.Seek(0, io.SeekStart); err != nil { panic(err) }

	var r io.Reader = f
	buf := []byte(".....")
	n, err := r.Read(buf)
	if err != nil { panic(err) }
	if n != 5 { panic("reader length mismatch") }
	if string(buf) != "hello" { panic("reader copy-back mismatch") }
	if err = f.Close(); err != nil { panic(err) }
}
`, path))
}

func TestIOWriterInterface(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writer.txt")
	testutil.Run(t, fmt.Sprintf(`
package main
import "io"
import "os"

func main() {
	f, err := os.Create(%q)
	if err != nil { panic(err) }

	var w io.Writer = f
	n, err := w.Write([]byte("hello"))
	if err != nil { panic(err) }
	if n != 5 { panic("writer length mismatch") }

	if _, err = f.Seek(0, io.SeekStart); err != nil { panic(err) }
	data, err := io.ReadAll(f)
	if err != nil { panic(err) }
	if string(data) != "hello" { panic("writer content mismatch") }
	if err = f.Close(); err != nil { panic(err) }
}
`, path))
}
