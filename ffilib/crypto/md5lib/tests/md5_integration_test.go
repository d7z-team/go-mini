package md5lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/crypto/md5lib"
)

func TestMD5Sum(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("crypto/md5", md5lib.MD5_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "sum",
			Imports: []string{"crypto/md5", "encoding/hex"},
			Expr:    `hex.EncodeToString(md5.Sum([]byte("abc")))`,
			Want:    "900150983cd24fb0d6963f7d28e17f72",
			Covers:  []string{"Sum"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
