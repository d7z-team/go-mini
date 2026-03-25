package strconvlib

import (
	"strconv"
)

type StrconvHost struct{}

func (h *StrconvHost) Atoi(s string) (int, error) { return strconv.Atoi(s) }
func (h *StrconvHost) Itoa(i int) string         { return strconv.Itoa(i) }
func (h *StrconvHost) ParseBool(str string) (bool, error) { return strconv.ParseBool(str) }
func (h *StrconvHost) ParseFloat(s string, bitSize int) (float64, error) {
	return strconv.ParseFloat(s, bitSize)
}
func (h *StrconvHost) ParseInt(s string, base int, bitSize int) (int64, error) {
	return strconv.ParseInt(s, base, bitSize)
}
func (h *StrconvHost) FormatBool(b bool) string { return strconv.FormatBool(b) }
func (h *StrconvHost) FormatFloat(f float64, format byte, prec, bitSize int) string {
	return strconv.FormatFloat(f, format, prec, bitSize)
}
func (h *StrconvHost) FormatInt(i int64, base int) string { return strconv.FormatInt(i, base) }
func (h *StrconvHost) Quote(s string) string             { return strconv.Quote(s) }
func (h *StrconvHost) Unquote(s string) (string, error)  { return strconv.Unquote(s) }
