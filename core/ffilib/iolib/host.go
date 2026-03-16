package iolib

import (
	"io"
)

type IOHost struct{}

func (h *IOHost) ReadAll(r any) ([]byte, error) {
	if reader, ok := r.(io.Reader); ok {
		return io.ReadAll(reader)
	}
	return nil, io.ErrUnexpectedEOF
}
