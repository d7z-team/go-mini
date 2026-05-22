package byteslib

import (
	"bytes"
)

type BytesHost struct{}

func (h *BytesHost) Contains(b, sub []byte) bool { return bytes.Contains(b, sub) }
func (h *BytesHost) Count(s, sep []byte) int     { return bytes.Count(s, sep) }
func (h *BytesHost) HasPrefix(s, prefix []byte) bool {
	return bytes.HasPrefix(s, prefix)
}

func (h *BytesHost) HasSuffix(s, suffix []byte) bool {
	return bytes.HasSuffix(s, suffix)
}
func (h *BytesHost) Index(s, sep []byte) int     { return bytes.Index(s, sep) }
func (h *BytesHost) LastIndex(s, sep []byte) int { return bytes.LastIndex(s, sep) }
func (h *BytesHost) ToLower(s []byte) []byte     { return bytes.ToLower(s) }
func (h *BytesHost) ToUpper(s []byte) []byte     { return bytes.ToUpper(s) }
func (h *BytesHost) Trim(s []byte, cutset string) []byte {
	return bytes.Trim(s, cutset)
}
func (h *BytesHost) TrimSpace(s []byte) []byte { return bytes.TrimSpace(s) }
func (h *BytesHost) Split(s, sep []byte) [][]byte {
	return bytes.Split(s, sep)
}

func (h *BytesHost) Join(s [][]byte, sep []byte) []byte {
	return bytes.Join(s, sep)
}
func (h *BytesHost) Repeat(b []byte, count int) []byte { return bytes.Repeat(b, count) }
func (h *BytesHost) ReplaceAll(s, old, replacement []byte) []byte {
	return bytes.ReplaceAll(s, old, replacement)
}
