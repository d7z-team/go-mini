package sha256lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestSHA256Sum(t *testing.T) {
	testutil.Run(t, `
package main
import "crypto/sha256"
import "encoding/hex"

func main() {
	if hex.EncodeToString(sha256.Sum256([]byte("abc"))) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		panic("sha256 failed")
	}
}
`)
}
