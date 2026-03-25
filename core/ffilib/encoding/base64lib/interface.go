//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg base64lib -path gopkg.d7z.net/go-mini/core/ffilib/encoding/base64lib -out base64_ffigen.go interface.go
package base64lib

// Base64 接口定义了 Base64 编码操作

// ffigen:module encoding/base64
type Base64 interface {
	EncodeToString(src []byte) string
	DecodeString(s string) ([]byte, error)
	URLEncodeToString(src []byte) string
	URLDecodeString(s string) ([]byte, error)
}
