package utf8lib

import (
	"unicode/utf8"
)

type UTF8Host struct{}

func (h *UTF8Host) DecodeRuneInString(s string) (int64, int) {
	r, size := utf8.DecodeRuneInString(s)
	return int64(r), size
}

func (h *UTF8Host) EncodeRune(r int64) []byte {
	p := make([]byte, utf8.UTFMax)
	n := utf8.EncodeRune(p, rune(r))
	return p[:n]
}

func (h *UTF8Host) FullRuneInString(s string) bool {
	return utf8.FullRuneInString(s)
}

func (h *UTF8Host) RuneCountInString(s string) int {
	return utf8.RuneCountInString(s)
}

func (h *UTF8Host) RuneLen(r int64) int {
	return utf8.RuneLen(rune(r))
}

func (h *UTF8Host) ValidString(s string) bool {
	return utf8.ValidString(s)
}
