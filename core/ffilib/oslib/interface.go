//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg oslib -path gopkg.d7z.net/go-mini/core/ffilib/oslib -out os_ffigen.go interface.go
package oslib

import (
	"gopkg.d7z.net/go-mini/core/ffilib/iolib"
)

// OS 接口定义了文件系统操作

// ffigen:module os
type OS interface {
	Open(name string) (*iolib.File, error)
	Create(name string) (*iolib.File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	Remove(name string) error
	Getenv(key string) string
}
