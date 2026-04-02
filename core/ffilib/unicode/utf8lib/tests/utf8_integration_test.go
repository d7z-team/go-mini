package utf8lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestRuneCountInString(t *testing.T) {
	testutil.Run(t, `
package main
import "unicode/utf8"

func main() {
	if utf8.RuneCountInString("你好") != 2 {
		panic("utf8.RuneCountInString failed")
	}
}
`)
}
