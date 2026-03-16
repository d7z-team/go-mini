package oslib

import (
	"context"
	"os"
	"unsafe"
)

type OSHost struct{}

func (h *OSHost) Open(ctx context.Context, name string) (*File, error) {
	// 在真实的 Go os 包中 Open 不带 ctx，这里仅作为演示接口兼容性
	f, err := os.Open(name)
	return (*File)(unsafe.Pointer(f)), err
}

func (h *OSHost) Create(name string) (*File, error) {
	f, err := os.Create(name)
	return (*File)(unsafe.Pointer(f)), err
}

func (h *OSHost) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (h *OSHost) WriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0644)
}

func (h *OSHost) Remove(name string) error {
	return os.Remove(name)
}

func (h *OSHost) Read(f *File, b []byte) (int, error) {
	if f == nil {
		return 0, os.ErrInvalid
	}
	return ((*os.File)(unsafe.Pointer(f))).Read(b)
}

func (h *OSHost) Write(f *File, b []byte) (int, error) {
	if f == nil {
		return 0, os.ErrInvalid
	}
	return ((*os.File)(unsafe.Pointer(f))).Write(b)
}

func (h *OSHost) Close(f *File) error {
	if f == nil {
		return nil
	}
	return ((*os.File)(unsafe.Pointer(f))).Close()
}
