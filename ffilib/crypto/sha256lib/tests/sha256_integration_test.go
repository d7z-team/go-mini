package sha256lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/crypto/sha256lib"
)

func TestSHA256Sum(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("crypto/sha256", sha256lib.SHA256_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "sum256",
			Imports: []string{"crypto/sha256", "encoding/hex"},
			Expr:    `hex.EncodeToString(sha256.Sum256([]byte("abc")))`,
			Want:    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			Covers:  []string{"Sum256"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
