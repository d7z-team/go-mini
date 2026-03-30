//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg iolib -module io -path gopkg.d7z.net/go-mini/core/ffilib/iolib -out io_ffigen.go interface.go host.go
package iolib

import (
	"context"
	"io"
)

// ffigen:module io
const (
	SeekStart   = io.SeekStart
	SeekCurrent = io.SeekCurrent
	SeekEnd     = io.SeekEnd
)

// ffigen:module io
type IO interface {
	// ReadAll 读取所有数据
	ReadAll(ctx context.Context, r any) ([]byte, error)
	// Copy 将 src 的数据拷贝到 dst
	Copy(ctx context.Context, dst, src any) (int64, error)
	// WriteString 将字符串写入 w
	WriteString(ctx context.Context, w any, s string) (int64, error)
}
