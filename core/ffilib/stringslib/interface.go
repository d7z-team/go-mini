//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg stringslib -path gopkg.d7z.net/go-mini/core/ffilib/stringslib -out strings_ffigen.go interface.go
package stringslib

// Strings 接口定义了字符串处理操作

// ffigen:module strings
type Strings interface {
	Contains(s, substr string) bool
	ContainsAny(s, chars string) bool
	Count(s, substr string) int
	HasPrefix(s, prefix string) bool
	HasSuffix(s, suffix string) bool
	Index(s, substr string) int
	LastIndex(s, substr string) int
	ToLower(s string) string
	ToUpper(s string) string
	Trim(s, cutset string) string
	TrimSpace(s string) string
	TrimPrefix(s, prefix string) string
	TrimSuffix(s, suffix string) string
	Replace(s, old, new string, n int) string
	ReplaceAll(s, old, new string) string
	Split(s, sep string) []string
	Join(elems []string, sep string) string
}
