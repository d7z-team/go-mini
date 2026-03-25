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
		
		if img.Width() != 10 || img.Height() != 10 {
			panic("Width/Height failed")
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
		if img2.Width() != 20 || img2.Height() != 20 {
			panic("Resize failed")
		}

		// 4. Encode and Decode
		pngData, err1 := img.EncodePNG()
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
		if img4.Width() != 5 || img4.Height() != 5 {
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

func TestImageLibraryV2(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "image"

	func main() {
		// 1. NewRGBA and Size
		img := image.NewRGBA(100, 50)
		w, h := img.Size()
		if w != 100 || h != 50 {
			panic("Size failed")
		}
		if img.Width() != 100 || img.Height() != 50 {
			panic("Width/Height failed")
		}

		// 2. Fill and At
		img.Fill(255, 128, 64, 255)
		r, g, b, a := img.At(50, 25)
		if r != 255 || g != 128 || b != 64 || a != 255 {
			panic("Fill failed")
		}

		// 3. Clear
		img.Clear()
		r2, g2, b2, a2 := img.At(50, 25)
		if r2 != 0 || g2 != 0 || b2 != 0 || a2 != 0 {
			panic("Clear failed")
		}

		// 4. Clone
		img.Set(10, 10, 1, 2, 3, 255)
		img2 := img.Clone()
		r3, g3, b3, a3 := img2.At(10, 10)
		if r3 != 1 || g3 != 2 || b3 != 3 || a3 != 255 {
			panic("Clone failed")
		}

		// 5. SubImage
		sub, err := img.SubImage(10, 10, 20, 20)
		if err != nil { panic(err) }
		if sub.Width() != 20 || sub.Height() != 20 {
			panic("SubImage Size failed")
		}
		r4, g4, b4, a4 := sub.At(10, 10)
		if r4 != 1 || g4 != 2 || b4 != 3 || a4 != 255 {
			panic("SubImage data failed")
		}

		// 6. Draw
		canvas := image.NewRGBA(10, 10)
		brush := image.NewRGBA(2, 2)
		brush.Fill(255, 0, 0, 255) // Solid Red
		
		canvas.Draw(brush, 4, 4) // Draw brush at (4,4)
		r5, g5, b5, a5 := canvas.At(5, 5)
		if r5 != 255 || g5 != 0 || b5 != 0 || a5 != 255 {
			panic("Draw failed at center")
		}
		
		// 7. Encode Methods
		jpegData, err4 := canvas.EncodeJPEG(80)
		if err4 != nil { panic(err4) }
		if len(jpegData) == 0 {
			panic("EncodeJPEG failed")
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
