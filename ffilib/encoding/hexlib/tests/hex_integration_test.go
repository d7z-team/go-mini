package hexlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/hexlib"
)

func TestHexSurfaceIntegration(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("encoding/hex", hexlib.SurfaceHex(&hexlib.HexHost{})),
	}, []testutil.Case{
		{
			Name:    "encode-decode-dump",
			Imports: []string{"encoding/hex"},
			Body: `
test.Out(hex.EncodeToString([]byte("abc")))
test.Out("|")
decoded, err := hex.DecodeString("616263")
if err != nil {
	panic(err)
}
test.OutBytes(decoded)
test.Out("|")
test.Out(hex.Dump([]byte("abc")))
`,
			Want:   "616263|abc|00000000  61 62 63                                          |abc|\n",
			Covers: []string{"EncodeToString", "DecodeString", "Dump"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
