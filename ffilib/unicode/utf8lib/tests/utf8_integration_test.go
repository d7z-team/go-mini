package utf8lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/unicode/utf8lib"
)

func TestUTF8(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("unicode/utf8", utf8lib.UTF8_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "string-and-rune-helpers",
			Imports: []string{"unicode/utf8"},
			Body: `
r, size := utf8.DecodeRuneInString("你好")
test.OutInt(r)
test.Out("|")
test.OutInt(size)
test.Out("|")
test.OutBytes(utf8.EncodeRune(22909))
test.Out("|")
test.OutBool(utf8.FullRuneInString("你"))
test.Out("|")
test.OutInt(utf8.RuneCountInString("你好"))
test.Out("|")
test.OutInt(utf8.RuneLen(20320))
test.Out("|")
test.OutBool(utf8.ValidString("你好"))
`,
			Want:   "20320|3|好|true|2|3|true",
			Covers: []string{"DecodeRuneInString", "EncodeRune", "FullRuneInString", "RuneCountInString", "RuneLen", "ValidString"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
