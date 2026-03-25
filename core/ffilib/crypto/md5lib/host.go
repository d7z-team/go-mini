package md5lib

import (
	"crypto/md5"
)

type MD5Host struct{}

func (h *MD5Host) Sum(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}
