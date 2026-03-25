package imagelib

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"unsafe"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ImageHost struct{}

func (h *ImageHost) NewRGBA(ctx context.Context, width, height int) *Image {
	if width <= 0 || height <= 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	return (*Image)(unsafe.Pointer(img))
}

func (h *ImageHost) Decode(ctx context.Context, data []byte) (*Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return (*Image)(unsafe.Pointer(rgba)), format, nil
}

func (h *ImageHost) EncodePNG(ctx context.Context, img *Image) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	var buf bytes.Buffer
	if err := png.Encode(&buf, native); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *ImageHost) EncodeJPEG(ctx context.Context, img *Image, quality int) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	var buf bytes.Buffer
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	if err := jpeg.Encode(&buf, native, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type ImageMethodsHost struct{}

func (h *ImageMethodsHost) Bounds(img *Image) (int, int, int, int) {
	if img == nil {
		return 0, 0, 0, 0
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	b := native.Bounds()
	return b.Min.X, b.Min.Y, b.Max.X, b.Max.Y
}

func (h *ImageMethodsHost) At(img *Image, x, y int) (int, int, int, int) {
	if img == nil {
		return 0, 0, 0, 0
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	c := native.RGBAAt(x, y)
	return int(c.R), int(c.G), int(c.B), int(c.A)
}

func (h *ImageMethodsHost) Set(img *Image, x, y, r, g, b, a int) {
	if img == nil {
		return
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	native.SetRGBA(x, y, color.RGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(a),
	})
}

func (h *ImageMethodsHost) Resize(img *Image, width, height int) (*Image, error) {
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid dimensions: %dx%d", width, height)
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	newImg := image.NewRGBA(image.Rect(0, 0, width, height))

	oldBounds := native.Bounds()
	oldWidth := oldBounds.Dx()
	oldHeight := oldBounds.Dy()

	// Nearest neighbor resize
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := x * oldWidth / width
			srcY := y * oldHeight / height
			newImg.Set(x, y, native.At(srcX+oldBounds.Min.X, srcY+oldBounds.Min.Y))
		}
	}

	return (*Image)(unsafe.Pointer(newImg)), nil
}

func (h *ImageMethodsHost) Crop(img *Image, x, y, width, height int) (*Image, error) {
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	native := (*image.RGBA)(unsafe.Pointer(img))
	bounds := native.Bounds()

	// Ensure crop area is within bounds
	rect := image.Rect(x, y, x+width, y+height).Intersect(bounds)
	if rect.Empty() {
		return nil, fmt.Errorf("crop area out of bounds or empty")
	}

	newImg := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(newImg, newImg.Bounds(), native, rect.Min, draw.Src)

	return (*Image)(unsafe.Pointer(newImg)), nil
}

// RegisterImage 为方便外部调用提供的注册函数
func RegisterImage(executor interface {
	RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string)
	RegisterStructSpec(string, ast.GoMiniType)
}, impl ImageLib, methods ImageMethods, registry *ffigo.HandleRegistry) {
	RegisterImageLib(executor, impl, registry)
	RegisterImageMethods(executor, methods, registry)
}
