package imagelib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register gif format
	"image/jpeg"
	"image/png"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// Image 是图像句柄 (对应 Go 的 image.RGBA)
// ffigen:methods
type Image struct {
	RGBA *image.RGBA
}

// Bounds 返回 x1, y1, x2, y2 (对应 Go 的 Bounds() Rectangle)
func (i *Image) Bounds() (int, int, int, int) {
	if i == nil || i.RGBA == nil {
		return 0, 0, 0, 0
	}
	b := i.RGBA.Bounds()
	return b.Min.X, b.Min.Y, b.Max.X, b.Max.Y
}

// Size 返回 width, height
func (i *Image) Size() (int, int) {
	if i == nil || i.RGBA == nil {
		return 0, 0
	}
	b := i.RGBA.Bounds()
	return b.Dx(), b.Dy()
}

// Width 返回图像宽度
func (i *Image) Width() int {
	if i == nil || i.RGBA == nil {
		return 0
	}
	return i.RGBA.Bounds().Dx()
}

// Height 返回图像高度
func (i *Image) Height() int {
	if i == nil || i.RGBA == nil {
		return 0
	}
	return i.RGBA.Bounds().Dy()
}

// At 返回 r, g, b, a (0-255)
func (i *Image) At(x, y int) (int, int, int, int) {
	if i == nil || i.RGBA == nil {
		return 0, 0, 0, 0
	}
	c := i.RGBA.RGBAAt(x, y)
	return int(c.R), int(c.G), int(c.B), int(c.A)
}

// Set 设置指定像素的颜色
func (i *Image) Set(x, y, r, g, b, a int) {
	if i == nil || i.RGBA == nil {
		return
	}
	i.RGBA.SetRGBA(x, y, color.RGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(a),
	})
}

// Fill 用指定颜色填充整个图像
func (i *Image) Fill(r, g, b, a int) {
	if i == nil || i.RGBA == nil {
		return
	}
	draw.Draw(i.RGBA, i.RGBA.Bounds(), &image.Uniform{C: color.RGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(a),
	}}, image.Point{}, draw.Src)
}

// Clear 将图像清空为透明
func (i *Image) Clear() {
	if i == nil || i.RGBA == nil {
		return
	}
	draw.Draw(i.RGBA, i.RGBA.Bounds(), image.Transparent, image.Point{}, draw.Src)
}

// Clone 复制当前图像
func (i *Image) Clone() *Image {
	if i == nil || i.RGBA == nil {
		return nil
	}
	rgba := image.NewRGBA(i.RGBA.Bounds())
	draw.Draw(rgba, rgba.Bounds(), i.RGBA, i.RGBA.Bounds().Min, draw.Src)
	return &Image{RGBA: rgba}
}

// SubImage 返回图像的子部分 (共享内存)
func (i *Image) SubImage(x, y, width, height int) (*Image, error) {
	if i == nil || i.RGBA == nil {
		return nil, errors.New("nil image")
	}
	rect := image.Rect(x, y, x+width, y+height).Intersect(i.RGBA.Bounds())
	if rect.Empty() {
		return nil, errors.New("sub-image area out of bounds or empty")
	}
	sub := i.RGBA.SubImage(rect).(*image.RGBA)
	return &Image{RGBA: sub}, nil
}

// Draw 将另一张图像绘制到当前图像上 (支持透明度叠加)
func (i *Image) Draw(_ context.Context, src *Image, x, y int) {
	if i == nil || i.RGBA == nil || src == nil || src.RGBA == nil {
		return
	}
	sr := src.RGBA.Bounds()
	dr := sr.Add(image.Point{X: x, Y: y}).Intersect(i.RGBA.Bounds())
	if dr.Empty() {
		return
	}
	draw.Draw(i.RGBA, dr, src.RGBA, sr.Min, draw.Over)
}

// Resize 缩放图像
func (i *Image) Resize(width, height int) (*Image, error) {
	if i == nil || i.RGBA == nil {
		return nil, errors.New("nil image")
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid dimensions: %dx%d", width, height)
	}

	newImg := image.NewRGBA(image.Rect(0, 0, width, height))
	oldBounds := i.RGBA.Bounds()
	oldWidth := oldBounds.Dx()
	oldHeight := oldBounds.Dy()

	// Nearest neighbor resize
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := x * oldWidth / width
			srcY := y * oldHeight / height
			newImg.Set(x, y, i.RGBA.At(srcX+oldBounds.Min.X, srcY+oldBounds.Min.Y))
		}
	}

	return &Image{RGBA: newImg}, nil
}

// Crop 裁剪图像
func (i *Image) Crop(x, y, width, height int) (*Image, error) {
	if i == nil || i.RGBA == nil {
		return nil, errors.New("nil image")
	}
	rect := image.Rect(x, y, x+width, y+height).Intersect(i.RGBA.Bounds())
	if rect.Empty() {
		return nil, errors.New("crop area out of bounds or empty")
	}

	newImg := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(newImg, newImg.Bounds(), i.RGBA, rect.Min, draw.Src)

	return &Image{RGBA: newImg}, nil
}

// EncodePNG 将图像编码为 PNG 字节数组
func (i *Image) EncodePNG() ([]byte, error) {
	if i == nil || i.RGBA == nil {
		return nil, errors.New("nil image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, i.RGBA); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EncodeJPEG 将图像编码为 JPEG 字节数组
func (i *Image) EncodeJPEG(quality int) ([]byte, error) {
	if i == nil || i.RGBA == nil {
		return nil, errors.New("nil image")
	}
	var buf bytes.Buffer
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	if err := jpeg.Encode(&buf, i.RGBA, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type ImageHost struct{}

func (h *ImageHost) NewRGBA(_ context.Context, width, height int) *Image {
	if width <= 0 || height <= 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	return &Image{RGBA: img}
}

func (h *ImageHost) Decode(_ context.Context, data []byte) (*Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return &Image{RGBA: rgba}, format, nil
}

// RegisterImageAll 为方便外部调用提供的注册函数
func RegisterImageAll(executor interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
	RegisterStructSchema(string, *runtime.RuntimeStructSpec)
	RegisterConstant(string, string)
}, impl ImageLib, registry *ffigo.HandleRegistry,
) {
	RegisterImageLib(executor, impl, registry)
	RegisterImage(executor, registry)
}
