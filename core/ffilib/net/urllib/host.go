package urllib

import (
	"net/url"
)

type URLHost struct{}

func (h *URLHost) QueryEscape(s string) string { return url.QueryEscape(s) }
func (h *URLHost) QueryUnescape(s string) (string, error) {
	return url.QueryUnescape(s)
}
func (h *URLHost) JoinPath(base string, elem ...string) string {
	res, _ := url.JoinPath(base, elem...)
	return res
}
