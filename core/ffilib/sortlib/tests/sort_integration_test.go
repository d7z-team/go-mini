package sortlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestInts(t *testing.T) {
	testutil.Run(t, `
package main
import "sort"

func main() {
	ints := sort.Ints([]Int64{3, 1, 2})
	if ints[0] != 1 || ints[1] != 2 || ints[2] != 3 {
		panic("sort.Ints failed")
	}
}
`)
}
