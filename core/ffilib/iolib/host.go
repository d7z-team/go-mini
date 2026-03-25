package iolib

import (
	"io"
	"os"
	"unsafe"
)

type IOHost struct{}

func (h *IOHost) ReadAll(r any) ([]byte, error) {
	if reader, ok := r.(io.Reader); ok {
		return io.ReadAll(reader)
	}
	return nil, io.ErrUnexpectedEOF
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
