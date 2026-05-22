//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg urllib -out url_ffigen.go interface.go
package urllib

// URL 接口定义了 URL 处理操作

// ffigen:module net/url
type URL interface {
	QueryEscape(s string) string
	QueryUnescape(s string) (string, error)
	JoinPath(base string, elem ...string) string
}
