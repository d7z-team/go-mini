package base64lib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/ffilib/internal/testutil"
)

func TestEncodeToString(t *testing.T) {
	testutil.Run(t, `
package main
import "encoding/base64"

func main() {
	if base64.EncodeToString([]byte("hello")) != "aGVsbG8=" {
		panic("base64 failed")
	}
}
`)
}
