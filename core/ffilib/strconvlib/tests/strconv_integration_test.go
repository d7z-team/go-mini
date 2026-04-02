package strconvlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestAtoiItoa(t *testing.T) {
	testutil.Run(t, `
package main
import "strconv"

func main() {
	v, err := strconv.Atoi("456")
	if err != nil || v != 456 || strconv.Itoa(123) != "123" {
		panic("strconv failed")
	}
}
`)
}
