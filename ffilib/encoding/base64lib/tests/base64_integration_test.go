package base64lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/base64lib"
)

func TestBase64(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("encoding/base64", base64lib.Base64_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "standard-and-url-encoding",
			Imports: []string{"encoding/base64"},
			Body: `
test.Out(base64.EncodeToString([]byte("hello")))
test.Out("|")
decoded, err := base64.DecodeString("aGVsbG8=")
if err != nil {
	panic(err)
}
test.OutBytes(decoded)
test.Out("|")
test.Out(base64.URLEncodeToString([]byte("a+b?")))
test.Out("|")
urlDecoded, err := base64.URLDecodeString("YStiPw==")
if err != nil {
	panic(err)
}
test.OutBytes(urlDecoded)
`,
			Want:   "aGVsbG8=|hello|YStiPw==|a+b?",
			Covers: []string{"EncodeToString", "DecodeString", "URLEncodeToString", "URLDecodeString"},
		},
	}, testutil.WithRegister(ffilib.RegisterAll))
}
