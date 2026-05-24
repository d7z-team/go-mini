package fmtlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/fmtlib"
)

func TestFmt(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("fmt", fmtlib.Surface(&fmtlib.FmtHost{})),
	}, []testutil.Case{
		{
			Name:    "print-and-format",
			Imports: []string{"fmt"},
			Body: `
fmt.Print("")
fmt.Println("")
fmt.Printf("%s", "")
test.Out(fmt.Sprintf("%s-%d", "mini", 7))
`,
			Want:   "mini-7",
			Covers: []string{"Print", "Println", "Printf", "Sprintf"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
