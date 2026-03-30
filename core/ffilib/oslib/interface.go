//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg oslib -module os -path gopkg.d7z.net/go-mini/core/ffilib/oslib -out os_ffigen.go interface.go
package oslib

import (
	"os"

	"gopkg.d7z.net/go-mini/core/ffilib/iolib"
)

// OS 接口定义了文件系统操作

// ffigen:module os
const (
	O_RDONLY = os.O_RDONLY
	O_WRONLY = os.O_WRONLY
	O_RDWR   = os.O_RDWR
	O_APPEND = os.O_APPEND
	O_CREATE = os.O_CREATE
	O_EXCL   = os.O_EXCL
	O_SYNC   = os.O_SYNC
	O_TRUNC  = os.O_TRUNC

	PathSeparator     = os.PathSeparator
	PathListSeparator = os.PathListSeparator
	DevNull           = os.DevNull
)

// ffigen:module os
type OS interface {
	Open(name string) (*iolib.File, error)
	Create(name string) (*iolib.File, error)
	OpenFile(name string, flag, perm int) (*iolib.File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	Remove(name string) error
	Getenv(key string) string
}
