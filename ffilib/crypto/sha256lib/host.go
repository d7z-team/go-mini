package sha256lib

import (
	"crypto/sha256"
)

type SHA256Host struct{}

func (h *SHA256Host) Sum256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
