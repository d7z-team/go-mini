package randlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/math/randlib"
)

func TestRand(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("math/rand", randlib.SurfaceRand(randlib.NewRandHost())),
	}, []testutil.Case{
		{
			Name:    "scalars-perm-and-read",
			Imports: []string{"math/rand"},
			Body: `
rand.Seed(1)
f32 := rand.Float32()
f64 := rand.Float64()
exp := rand.ExpFloat64()
i := rand.Int()
i31 := rand.Int31()
i31n := rand.Int31n(7)
in := rand.Intn(11)
i63 := rand.Int63()
i63n := rand.Int63n(13)
norm := rand.NormFloat64()
u32 := rand.Uint32()
u64 := rand.Uint64()
perm := rand.Perm(8)

validPerm := len(perm) == 8
for _, v := range perm {
	if v < 0 || v >= 8 {
		validPerm = false
	}
}

buf := []byte("......")
n, err := rand.Read(buf)
if err != nil {
	panic(err)
}
nonZero := false
for i := 0; i < len(buf); i++ {
	if buf[i] != 0 {
		nonZero = true
	}
}

test.OutBool(f32 >= 0 && f32 < 1)
test.Out("|")
test.OutBool(f64 >= 0 && f64 < 1)
test.Out("|")
test.OutBool(exp > 0)
test.Out("|")
test.OutBool(i >= 0)
test.Out("|")
test.OutBool(i31 >= 0)
test.Out("|")
test.OutBool(i31n >= 0 && i31n < 7)
test.Out("|")
test.OutBool(in >= 0 && in < 11)
test.Out("|")
test.OutBool(i63 >= 0)
test.Out("|")
test.OutBool(i63n >= 0 && i63n < 13)
test.Out("|")
test.OutBool(norm == norm)
test.Out("|")
test.OutBool(u32 == u32)
test.Out("|")
test.OutBool(u64 == u64)
test.Out("|")
test.OutBool(validPerm)
test.Out("|")
test.OutInt(n)
test.Out("|")
test.OutBool(nonZero)
`,
			Want:   "true|true|true|true|true|true|true|true|true|true|true|true|true|6|true",
			Covers: []string{"Seed", "Float32", "Float64", "ExpFloat64", "Int", "Int31", "Int31n", "Int63", "Int63n", "NormFloat64", "Uint32", "Uint64", "Perm", "Read", "Intn"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
