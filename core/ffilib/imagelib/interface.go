//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg imagelib -path gopkg.d7z.net/go-mini/core/ffilib/imagelib -out image_ffigen.go interface.go
package imagelib

import "context"

// Image 是一个占位符结构体，用于在 FFI 中表示图像句柄 (对应 Go 的 image.RGBA)
type Image struct{}

// ffigen:module image
type ImageLib interface {
	// Decode 对应 Go 的 image.Decode(r) (Image, string, error)
	Decode(ctx context.Context, data []byte) (*Image, string, error)
	// NewRGBA 对应 Go 的 image.NewRGBA(rect)
	NewRGBA(ctx context.Context, width, height int) *Image
	
	// 以下为方便脚本使用的扩展 API (Go 原生在 image/png 和 image/jpeg 中)
	EncodePNG(ctx context.Context, img *Image) ([]byte, error)
	EncodeJPEG(ctx context.Context, img *Image, quality int) ([]byte, error)
}

// ImageMethods 接口定义了图像句柄的方法

// ffigen:methods Image
type ImageMethods interface {
	// Bounds 返回 x1, y1, x2, y2 (对应 Go 的 Bounds() Rectangle)
	Bounds(img *Image) (int, int, int, int)
	// At 返回 r, g, b, a (0-255)
	At(img *Image, x, y int) (int, int, int, int)
	// Set 设置指定像素的颜色
	Set(img *Image, x, y, r, g, b, a int)
	
	// 高级操作 (宿主侧加速)
	Resize(img *Image, width, height int) (*Image, error)
	Crop(img *Image, x, y, width, height int) (*Image, error)
}
