package randlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestRandScalarAPIs(t *testing.T) {
	testutil.Run(t, `
package main
import "math/rand"

func main() {
	f32 := rand.Float32()
	if f32 < 0 || f32 >= 1 {
		panic("rand.Float32 out of range")
	}

	f64 := rand.Float64()
	if f64 < 0 || f64 >= 1 {
		panic("rand.Float64 out of range")
	}

	if rand.ExpFloat64() <= 0 {
		panic("rand.ExpFloat64 must be positive")
	}

	i31 := rand.Int31()
	if i31 < 0 {
		panic("rand.Int31 must be non-negative")
	}

	i31n := rand.Int31n(7)
	if i31n < 0 || i31n >= 7 {
		panic("rand.Int31n out of range")
	}

	in := rand.Intn(11)
	if in < 0 || in >= 11 {
		panic("rand.Intn out of range")
	}

	i63n := rand.Int63n(13)
	if i63n < 0 || i63n >= 13 {
		panic("rand.Int63n out of range")
	}

	if rand.Uint32() == 0 && rand.Uint64() == 0 && rand.Int() == 0 && rand.Int63() == 0 {
		// Avoid an always-true linter-style branch while still touching the APIs.
	}

	if rand.NormFloat64() == 0 && rand.NormFloat64() == 0 {
	}
}
`)
}

func TestRandPermAndRead(t *testing.T) {
	testutil.Run(t, `
package main
import "math/rand"

func main() {
	rand.Seed(1)

	perm := rand.Perm(8)
	if len(perm) != 8 {
		panic("rand.Perm length mismatch")
	}

	seen := map[int]bool{}
	for _, v := range perm {
		if v < 0 || v >= 8 {
			panic("rand.Perm element out of range")
		}
		if seen[v] {
			panic("rand.Perm duplicated element")
		}
		seen[v] = true
	}

	buf := []byte("......")
	n, err := rand.Read(buf)
	if err != nil {
		panic(err)
	}
	if n != len(buf) {
		panic("rand.Read length mismatch")
	}

	allZero := true
	for i := 0; i < len(buf); i++ {
		if buf[i] != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		panic("rand.Read did not fill buffer")
	}
}
`)
}
