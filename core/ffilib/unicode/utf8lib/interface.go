//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg utf8lib -out utf8_ffigen.go interface.go
package utf8lib

// UTF8 接口定义了 UTF-8 处理操作

// ffigen:module unicode/utf8
type UTF8 interface {
	DecodeRuneInString(s string) (int64, int)
	EncodeRune(r int64) []byte
	FullRuneInString(s string) bool
	RuneCountInString(s string) int
	RuneLen(r int64) int
	ValidString(s string) bool
}
