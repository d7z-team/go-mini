//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg fmtlib -out fmt_ffigen.go interface.go
package fmtlib

import "context"

// Fmt 接口定义了格式化输出操作

// ffigen:module fmt
type Fmt interface {
	Print(ctx context.Context, args ...any)
	Println(ctx context.Context, args ...any)
	Printf(ctx context.Context, format string, args ...any)
	Sprintf(ctx context.Context, format string, args ...any) string
}
