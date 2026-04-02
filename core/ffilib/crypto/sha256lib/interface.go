//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg sha256lib -out sha256_ffigen.go interface.go
package sha256lib

import (
	"crypto/sha256"
)

// SHA256 接口定义了 SHA256 哈希操作

// ffigen:module crypto/sha256
const (
	Size      = sha256.Size
	BlockSize = sha256.BlockSize
)

// ffigen:module crypto/sha256
type SHA256 interface {
	Sum256(data []byte) []byte
}
