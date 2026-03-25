//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg iolib -path gopkg.d7z.net/go-mini/core/ffilib/iolib -out io_ffigen.go interface.go
package iolib

// File 是一个占位符结构体，用于在 FFI 中表示文件句柄
type File struct{}

// IO 接口定义了 I/O 操作

// ffigen:module io
type IO interface {
	ReadAll(r any) ([]byte, error)
}

// FileMethods 接口定义了文件句柄的方法

// ffigen:methods File
type FileMethods interface {
	Read(f *File, b []byte) (int64, error)
	Write(f *File, b []byte) (int64, error)
	Close(f *File) error
}
