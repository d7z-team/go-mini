//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg hexlib -out hex_ffigen.go interface.go
package hexlib

// Hex 接口定义了 Hex 编码操作

// ffigen:module encoding/hex
type Hex interface {
	EncodeToString(src []byte) string
	DecodeString(s string) ([]byte, error)
	Dump(data []byte) string
}
