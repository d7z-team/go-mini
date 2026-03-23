//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg oslib -out os_ffigen.go interface.go
package oslib

import "context"

// File 是一个占位符结构体，用于在 FFI 中表示文件句柄
type File struct{}

// OS 接口定义了文件系统操作

// ffigen:module os
type OS interface {
	Open(ctx context.Context, name string) (*File, error)
	Create(ctx context.Context, name string) (*File, error)
	ReadFile(ctx context.Context, name string) ([]byte, error)
	WriteFile(ctx context.Context, name string, data []byte) error
	Remove(ctx context.Context, name string) error
	Getenv(key string) string
	Setenv(key, value string) error
}

// FileMethods 接口定义了文件句柄的方法

// ffigen:methods File
type FileMethods interface {
	Read(f *File, b []byte) (int64, error)
	Write(f *File, b []byte) (int64, error)
	Close(f *File) error
}
