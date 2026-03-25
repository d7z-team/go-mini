package oslib

import (
	"os"

	"gopkg.d7z.net/go-mini/core/ffilib/iolib"
)

type OSHost struct{}

func (h *OSHost) Open(name string) (*iolib.File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return &iolib.File{F: f}, nil
}

func (h *OSHost) Create(name string) (*iolib.File, error) {
	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	return &iolib.File{F: f}, nil
}

func (h *OSHost) OpenFile(name string, flag, perm int) (*iolib.File, error) {
	f, err := os.OpenFile(name, flag, os.FileMode(perm))
	if err != nil {
		return nil, err
	}
	return &iolib.File{F: f}, nil
}

func (h *OSHost) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (h *OSHost) WriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0o644)
}

func (h *OSHost) Remove(name string) error {
	return os.Remove(name)
}

func (h *OSHost) Getenv(key string) string {
	return os.Getenv(key)
}
