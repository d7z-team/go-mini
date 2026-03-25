package base64lib

import (
	"encoding/base64"
)

type Base64Host struct{}

func (h *Base64Host) EncodeToString(src []byte) string {
	return base64.StdEncoding.EncodeToString(src)
}

func (h *Base64Host) DecodeString(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func (h *Base64Host) URLEncodeToString(src []byte) string {
	return base64.URLEncoding.EncodeToString(src)
}

func (h *Base64Host) URLDecodeString(s string) ([]byte, error) {
	return base64.URLEncoding.DecodeString(s)
}
