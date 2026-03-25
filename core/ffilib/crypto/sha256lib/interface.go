//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg sha256lib -path gopkg.d7z.net/go-mini/core/ffilib/crypto/sha256lib -out sha256_ffigen.go interface.go
package sha256lib

// SHA256 接口定义了 SHA256 哈希操作

// ffigen:module crypto/sha256
type SHA256 interface {
	Sum256(data []byte) []byte
}
