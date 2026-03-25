package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestImageLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "image"

	func main() {
		// 1. NewRGBA
		img := image.NewRGBA(10, 10)
		x1, y1, x2, y2 := img.Bounds()
		if x1 != 0 || y1 != 0 || x2 != 10 || y2 != 10 {
			panic("Bounds failed")
		}

		// 2. Set and At
		img.Set(0, 0, 255, 0, 0, 255) // Red
		r, g, b, a := img.At(0, 0)
		if r != 255 || g != 0 || b != 0 || a != 255 {
			panic("At failed")
		}

		// 3. Resize
		img2, err := img.Resize(20, 20)
		if err != nil { panic(err) }
		_, _, x2_2, y2_2 := img2.Bounds()
		if x2_2 != 20 || y2_2 != 20 {
			panic("Resize failed")
		}

		// 4. Encode and Decode
		pngData, err1 := image.EncodePNG(img)
		if err1 != nil { panic(err1) }
		
		img3, format, err2 := image.Decode(pngData)
		if err2 != nil { panic(err2) }
		if format != "png" {
			panic("Decode format mismatch: " + format)
		}
		
		r3, g3, b3, a3 := img3.At(0, 0)
		if r3 != 255 || g3 != 0 || b3 != 0 || a3 != 255 {
			panic("Decode data mismatch")
		}

		// 5. Crop
		img4, err3 := img.Crop(0, 0, 5, 5)
		if err3 != nil { panic(err3) }
		_, _, x2_4, y2_4 := img4.Bounds()
		if x2_4 != 5 || y2_4 != 5 {
			panic("Crop failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
