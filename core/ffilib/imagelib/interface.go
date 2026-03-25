//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg imagelib -path gopkg.d7z.net/go-mini/core/ffilib/imagelib -out image_ffigen.go interface.go host.go
package imagelib

import "context"

// ffigen:module image
type ImageLib interface {
	// Decode 对应 Go 的 image.Decode(r) (Image, string, error)
	Decode(ctx context.Context, data []byte) (*Image, string, error)
	// NewRGBA 对应 Go 的 image.NewRGBA(rect)
	NewRGBA(ctx context.Context, width, height int) *Image
}
