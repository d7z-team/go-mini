package imagelib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/imagelib"
)

func TestImageSurfaceIntegration(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("image", imagelib.SurfaceImageLib(&imagelib.ImageHost{})),
		testutil.SurfaceFFISchema("image.Image", imagelib.SurfaceImage()),
	}, []testutil.Case{
		{
			Name:    "image-operations-and-codecs",
			Imports: []string{"image"},
			Body: `
img := image.NewRGBA(4, 3)
img.Fill(10, 20, 30, 255)
img.Set(1, 1, 255, 0, 0, 255)
r, g, b, a := img.At(1, 1)
x1, y1, x2, y2 := img.Bounds()
w, h := img.Size()

clone := img.Clone()
clone.Clear()
cr, cg, cb, ca := clone.At(1, 1)

sub, err := img.SubImage(1, 1, 2, 2)
if err != nil {
	panic(err)
}
crop, err := img.Crop(1, 1, 2, 2)
if err != nil {
	panic(err)
}
resized, err := img.Resize(2, 2)
if err != nil {
	panic(err)
}

overlay := image.NewRGBA(1, 1)
overlay.Fill(0, 255, 0, 255)
img.Draw(overlay, 0, 0)
dr, dg, db, da := img.At(0, 0)

pngData, err := img.EncodePNG()
if err != nil {
	panic(err)
}
decoded, format, err := image.Decode(pngData)
if err != nil {
	panic(err)
}
jpegData, err := img.EncodeJPEG(80)
if err != nil {
	panic(err)
}

test.OutInt(img.Width())
test.Out("|")
test.OutInt(img.Height())
test.Out("|")
test.OutInt(w)
test.Out(",")
test.OutInt(h)
test.Out("|")
test.OutInt(x1)
test.Out(",")
test.OutInt(y1)
test.Out(",")
test.OutInt(x2)
test.Out(",")
test.OutInt(y2)
test.Out("|")
test.OutInt(r)
test.Out(",")
test.OutInt(g)
test.Out(",")
test.OutInt(b)
test.Out(",")
test.OutInt(a)
test.Out("|")
test.OutInt(cr)
test.Out(",")
test.OutInt(cg)
test.Out(",")
test.OutInt(cb)
test.Out(",")
test.OutInt(ca)
test.Out("|")
test.OutInt(sub.Width())
test.Out(",")
test.OutInt(crop.Height())
test.Out(",")
test.OutInt(resized.Width())
test.Out("|")
test.OutInt(dr)
test.Out(",")
test.OutInt(dg)
test.Out(",")
test.OutInt(db)
test.Out(",")
test.OutInt(da)
test.Out("|")
test.Out(format)
test.Out("|")
test.OutInt(decoded.Width())
test.Out("|")
test.OutBool(len(pngData) > 0)
test.Out("|")
test.OutBool(len(jpegData) > 0)
`,
			Want:   "4|3|4,3|0,0,4,3|255,0,0,255|0,0,0,0|2,2,2|0,255,0,255|png|4|true|true",
			Covers: []string{"Decode", "NewRGBA", "Bounds", "Size", "Width", "Height", "At", "Set", "Fill", "Clear", "Clone", "SubImage", "Draw", "Resize", "Crop", "EncodePNG", "EncodeJPEG"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
