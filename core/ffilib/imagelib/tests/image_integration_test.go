package imagelib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestRGBAEncodeDecode(t *testing.T) {
	testutil.Run(t, `
package main
import "image"

func main() {
	img := image.NewRGBA(2, 2)
	img.Set(0, 0, 255, 0, 0, 255)

	r, g, b, a := img.At(0, 0)
	if r != 255 || g != 0 || b != 0 || a != 255 {
		panic("At failed")
	}

	data, err := img.EncodePNG()
	if err != nil || len(data) == 0 {
		panic("EncodePNG failed")
	}

	decoded, format, err := image.Decode(data)
	if err != nil || format != "png" {
		panic("Decode failed")
	}

	r2, g2, b2, a2 := decoded.At(0, 0)
	if r2 != 255 || g2 != 0 || b2 != 0 || a2 != 255 {
		panic("Decode pixel mismatch")
	}
}
`)
}
