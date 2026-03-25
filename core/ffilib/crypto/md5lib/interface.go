//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg md5lib -path gopkg.d7z.net/go-mini/core/ffilib/crypto/md5lib -out md5_ffigen.go interface.go
package md5lib

// MD5 接口定义了 MD5 哈希操作

// ffigen:module crypto/md5
type MD5 interface {
	Sum(data []byte) []byte
}
