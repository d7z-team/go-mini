package mathlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestFunctionsAndConstants(t *testing.T) {
	testutil.Run(t, `
package main
import "math"

func main() {
	if math.Abs(-1.0) != 1.0 || math.Sqrt(4.0) != 2.0 {
		panic("math function failed")
	}
	if math.Pi < 3.14 || math.E < 2.71 {
		panic("math constant failed")
	}
}
`)
}
