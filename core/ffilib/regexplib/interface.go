//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg regexplib -path gopkg.d7z.net/go-mini/core/ffilib/regexplib -out regexp_ffigen.go interface.go
package regexplib

// Regexp 接口定义了正则表达式操作

// ffigen:module regexp
type Regexp interface {
	Match(pattern string, b []byte) (bool, error)
	MatchString(pattern string, s string) (bool, error)
	QuoteMeta(s string) string
	FindString(pattern string, s string) string
	FindStringSubmatch(pattern string, s string) []string
	ReplaceAllString(pattern string, src, repl string) (string, error)
	Split(pattern string, s string, n int) ([]string, error)
}
