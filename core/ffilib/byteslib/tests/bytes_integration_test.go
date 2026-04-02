package byteslib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestBytesContains(t *testing.T) {
	testutil.Run(t, `
package main
import "bytes"

func main() {
	if !bytes.Contains([]byte("hello"), []byte("ell")) {
		panic("bytes.Contains failed")
	}
}
`)
}
