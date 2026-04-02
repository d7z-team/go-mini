package randlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestFloat64Range(t *testing.T) {
	testutil.Run(t, `
package main
import "math/rand"

func main() {
	rand.Seed(1)
	v := rand.Float64()
	if v < 0 || v > 1 {
		panic("rand.Float64 out of range")
	}
}
`)
}
