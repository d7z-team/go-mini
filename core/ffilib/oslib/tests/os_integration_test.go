package oslib_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestFileLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os.txt")
	testutil.Run(t, fmt.Sprintf(`
package main
import "os"

func main() {
	if err := os.WriteFile(%q, []byte("hello")); err != nil { panic(err) }
	data, err := os.ReadFile(%q)
	if err != nil || string(data) != "hello" { panic("ReadFile failed") }
	if err := os.Remove(%q); err != nil { panic(err) }
}
`, path, path, path))
}

func TestGetenvAndConstants(t *testing.T) {
	t.Setenv("GO_MINI_TEST", "rocks")
	testutil.Run(t, `
package main
import "os"

func main() {
	if os.Getenv("GO_MINI_TEST") != "rocks" {
		panic("Getenv failed")
	}
	if (os.O_CREATE | os.O_RDWR) == 0 {
		panic("os constants missing")
	}
}
`)
}
