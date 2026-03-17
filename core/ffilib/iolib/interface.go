//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg iolib -out io_ffigen.go interface.go
package iolib

// IO 接口定义了 I/O 操作
//
// ffigen:module io
type IO interface {
	ReadAll(r any) ([]byte, error)
}
