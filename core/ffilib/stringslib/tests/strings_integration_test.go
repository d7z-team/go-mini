package stringslib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestTrimContainsJoin(t *testing.T) {
	testutil.Run(t, `
package main
import "strings"

func main() {
	s := strings.TrimSpace("  hello  ")
	if s != "hello" || !strings.Contains(s, "ell") || strings.Join([]string{"a", "b"}, "-") != "a-b" {
		panic("strings failed")
	}
}
`)
}
