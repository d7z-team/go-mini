//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg regexplib -out regexp_ffigen.go interface.go
package regexplib

// Regexp 接口定义了正则表达式操作

// ffigen:module regexp
type Regexp interface {
	Match(pattern string, b []byte) (bool, error)
	MatchString(pattern, s string) (bool, error)
	QuoteMeta(s string) string
	FindString(pattern, s string) string
	FindStringSubmatch(pattern, s string) []string
	ReplaceAllString(pattern, src, repl string) (string, error)
	Split(pattern, s string, n int) ([]string, error)
}
