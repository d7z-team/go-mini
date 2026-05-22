package byteslib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/byteslib"
)

func TestBytes(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("bytes", byteslib.Bytes_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "predicates-and-index",
			Imports: []string{"bytes"},
			Body: `
test.OutBool(bytes.Contains([]byte("hello"), []byte("ell")))
test.Out("|")
test.OutInt(bytes.Count([]byte("banana"), []byte("na")))
test.Out("|")
test.OutBool(bytes.HasPrefix([]byte("gopher"), []byte("go")))
test.Out("|")
test.OutBool(bytes.HasSuffix([]byte("gopher"), []byte("her")))
test.Out("|")
test.OutInt(bytes.Index([]byte("gopher"), []byte("op")))
test.Out("|")
test.OutInt(bytes.LastIndex([]byte("gopher-gopher"), []byte("go")))
`,
			Want:   "true|2|true|true|1|7",
			Covers: []string{"Contains", "Count", "HasPrefix", "HasSuffix", "Index", "LastIndex"},
		},
		{
			Name:    "transform-and-compose",
			Imports: []string{"bytes"},
			Body: `
test.OutBytes(bytes.ToLower([]byte("GO")))
test.Out("|")
test.OutBytes(bytes.ToUpper([]byte("go")))
test.Out("|")
test.OutBytes(bytes.Trim([]byte("!!go!!"), "!"))
test.Out("|")
test.OutBytes(bytes.TrimSpace([]byte("  go  ")))
test.Out("|")
test.OutBytes(bytes.Join(bytes.Split([]byte("a,b,c"), []byte(",")), []byte("/")))
test.Out("|")
test.OutBytes(bytes.Repeat([]byte("ab"), 2))
test.Out("|")
test.OutBytes(bytes.ReplaceAll([]byte("a-a"), []byte("a"), []byte("b")))
`,
			Want:   "go|GO|go|go|a/b/c|abab|b-b",
			Covers: []string{"ToLower", "ToUpper", "Trim", "TrimSpace", "Split", "Join", "Repeat", "ReplaceAll"},
		},
	}, testutil.WithRegister(ffilib.RegisterAll))
}
