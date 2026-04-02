package regexplib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestMatchString(t *testing.T) {
	testutil.Run(t, `
package main
import "regexp"

func main() {
	ok, err := regexp.MatchString("a.c", "abc")
	if err != nil || !ok {
		panic("regexp.MatchString failed")
	}
}
`)
}
