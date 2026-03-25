package oslib

import (
	"os"
	"unsafe"
)

type OSHost struct{}

func (h *OSHost) Open(name string) (*File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return (*File)(unsafe.Pointer(f)), nil
}

func (h *OSHost) Create(name string) (*File, error) {
	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	return (*File)(unsafe.Pointer(f)), nil
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

type FileMethodsHost struct{}

func (h *FileMethodsHost) Read(f *File, b []byte) (int64, error) {
	if f == nil {
		return 0, os.ErrInvalid
	}
	n, err := ((*os.File)(unsafe.Pointer(f))).Read(b)
	return int64(n), err
}

func (h *FileMethodsHost) Write(f *File, b []byte) (int64, error) {
	if f == nil {
		return 0, os.ErrInvalid
	}
	n, err := ((*os.File)(unsafe.Pointer(f))).Write(b)
	return int64(n), err
}

func (h *FileMethodsHost) Close(f *File) error {
	if f == nil {
		return nil
	}
	return ((*os.File)(unsafe.Pointer(f))).Close()
}

// 满足 io.Reader 接口
func (f *File) Read(p []byte) (n int, err error) {
	return ((*os.File)(unsafe.Pointer(f))).Read(p)
}

// 满足 io.Writer 接口
func (f *File) Write(p []byte) (n int, err error) {
	return ((*os.File)(unsafe.Pointer(f))).Write(p)
}
