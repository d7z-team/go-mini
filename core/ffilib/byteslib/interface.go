//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg byteslib -path gopkg.d7z.net/go-mini/core/ffilib/byteslib -out bytes_ffigen.go interface.go
package byteslib

// Bytes 接口定义了字节切片处理操作

// ffigen:module bytes
type Bytes interface {
	Contains(b, sub []byte) bool
	Count(s, sep []byte) int
	HasPrefix(s, prefix []byte) bool
	HasSuffix(s, suffix []byte) bool
	Index(s, sep []byte) int
	LastIndex(s, sep []byte) int
	ToLower(s []byte) []byte
	ToUpper(s []byte) []byte
	Trim(s []byte, cutset string) []byte
	TrimSpace(s []byte) []byte
	Split(s, sep []byte) [][]byte
	Join(s [][]byte, sep []byte) []byte
	Repeat(b []byte, count int) []byte
	ReplaceAll(s, old, new []byte) []byte
}
