package hexlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestEncodeToString(t *testing.T) {
	testutil.Run(t, `
package main
import "encoding/hex"

func main() {
	if hex.EncodeToString([]byte("abc")) != "616263" {
		panic("hex failed")
	}
}
`)
}
