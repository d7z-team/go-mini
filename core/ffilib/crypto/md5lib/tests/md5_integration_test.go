package md5lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestMD5Sum(t *testing.T) {
	testutil.Run(t, `
package main
import "crypto/md5"
import "encoding/hex"

func main() {
	if hex.EncodeToString(md5.Sum([]byte("abc"))) != "900150983cd24fb0d6963f7d28e17f72" {
		panic("md5 failed")
	}
}
`)
}
