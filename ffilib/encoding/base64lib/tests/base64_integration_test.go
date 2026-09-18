package base64lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/encoding/base64lib"
)

func TestBase64SurfaceIntegration(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("encoding/base64", base64lib.SurfaceModule(&base64lib.ModuleHost{})),
		testutil.SurfaceFFISchema("encoding/base64.Encoding", base64lib.SurfaceEncoding()),
	}, []testutil.Case{
		{
			Name:    "package-encoding-values",
			Imports: []string{"encoding/base64"},
			Body: `
test.Out(base64.StdEncoding.EncodeToString([]byte("hello")))
test.Out("|")
decoded, err := base64.StdEncoding.DecodeString("aGVsbG8=")
if err != nil {
	panic(err)
}
test.OutBytes(decoded)
test.Out("|")
test.Out(base64.URLEncoding.EncodeToString([]byte("a+b?")))
test.Out("|")
urlDecoded, err := base64.URLEncoding.DecodeString("YStiPw==")
if err != nil {
	panic(err)
}
test.OutBytes(urlDecoded)
test.Out("|")
test.Out(base64.RawStdEncoding.EncodeToString([]byte("hi")))
test.Out("|")
rawDecoded, err := base64.RawStdEncoding.DecodeString("aGk")
if err != nil {
	panic(err)
}
test.OutBytes(rawDecoded)
test.Out("|")
test.Out(base64.RawURLEncoding.EncodeToString([]byte("hi?")))
`,
			Want:   "aGVsbG8=|hello|YStiPw==|a+b?|aGk|hi|aGk_",
			Covers: []string{"EncodeToString", "DecodeString"},
		},
		{
			Name:    "new-encoding-and-buffer-methods",
			Imports: []string{"encoding/base64"},
			Body: `
enc := base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/").WithPadding(base64.NoPadding)
test.Out(enc.EncodeToString([]byte("hi")))
test.Out("|")
test.OutInt(enc.EncodedLen(2))
test.Out("|")
test.OutInt(enc.DecodedLen(3))
test.Out("|")
encoded := enc.AppendEncode([]byte("p:"), []byte("hi"))
test.OutBytes(encoded)
test.Out("|")
decoded, err := enc.AppendDecode([]byte("p:"), []byte("aGk"))
if err != nil {
	panic(err)
}
test.OutBytes(decoded)
test.Out("|")
strictDecoded, err := enc.Strict().DecodeString("aGk")
if err != nil {
	panic(err)
}
test.OutBytes(strictDecoded)
test.Out("|")
buf := make([]byte, enc.EncodedLen(2))
enc.Encode(buf, []byte("hi"))
test.OutBytes(buf)
test.Out("|")
raw := make([]byte, enc.DecodedLen(len(buf)))
n, err := enc.Decode(raw, buf)
if err != nil {
	panic(err)
}
test.OutInt(n)
test.Out(":")
test.OutBytes(raw[:n])
`,
			Want:   "aGk|3|2|p:aGk|p:hi|hi|aGk|2:hi",
			Covers: []string{"NewEncoding", "WithPadding", "EncodedLen", "DecodedLen", "AppendEncode", "AppendDecode", "Strict", "Encode", "Decode"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
