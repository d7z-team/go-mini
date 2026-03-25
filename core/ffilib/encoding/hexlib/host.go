package hexlib

import (
	"encoding/hex"
)

type HexHost struct{}

func (h *HexHost) EncodeToString(src []byte) string { return hex.EncodeToString(src) }
func (h *HexHost) DecodeString(s string) ([]byte, error) {
	return hex.DecodeString(s)
}
func (h *HexHost) Dump(data []byte) string { return hex.Dump(data) }
