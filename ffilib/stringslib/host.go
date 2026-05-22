package stringslib

import (
	"strings"
)

type StringsHost struct{}

func (h *StringsHost) Contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func (h *StringsHost) ContainsAny(s, chars string) bool {
	return strings.ContainsAny(s, chars)
}

func (h *StringsHost) Count(s, substr string) int {
	return strings.Count(s, substr)
}

func (h *StringsHost) HasPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix)
}

func (h *StringsHost) HasSuffix(s, suffix string) bool {
	return strings.HasSuffix(s, suffix)
}

func (h *StringsHost) Index(s, substr string) int {
	return strings.Index(s, substr)
}

func (h *StringsHost) LastIndex(s, substr string) int {
	return strings.LastIndex(s, substr)
}

func (h *StringsHost) ToLower(s string) string {
	return strings.ToLower(s)
}

func (h *StringsHost) ToUpper(s string) string {
	return strings.ToUpper(s)
}

func (h *StringsHost) Trim(s, cutset string) string {
	return strings.Trim(s, cutset)
}

func (h *StringsHost) TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

func (h *StringsHost) TrimPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

func (h *StringsHost) TrimSuffix(s, suffix string) string {
	return strings.TrimSuffix(s, suffix)
}

func (h *StringsHost) Replace(s, old, replacement string, n int) string {
	return strings.Replace(s, old, replacement, n)
}

func (h *StringsHost) ReplaceAll(s, old, replacement string) string {
	return strings.ReplaceAll(s, old, replacement)
}

func (h *StringsHost) Split(s, sep string) []string {
	return strings.Split(s, sep)
}

func (h *StringsHost) Join(elems []string, sep string) string {
	return strings.Join(elems, sep)
}
