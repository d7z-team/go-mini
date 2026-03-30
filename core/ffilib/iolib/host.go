package iolib

import (
	"bytes"
	"context"
	"io"
	"os"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type IOHost struct{}

// getReader 尝试从 any 转换为 io.Reader
func getReader(r any) io.Reader {
	if reader, ok := r.(io.Reader); ok {
		return reader
	}
	if f, ok := r.(*File); ok && f != nil && f.F != nil {
		return f.F
	}
	if b, ok := r.([]byte); ok {
		return bytes.NewReader(b)
	}
	return nil
}

// getWriter 尝试从 any 转换为 io.Writer
func getWriter(w any) io.Writer {
	if writer, ok := w.(io.Writer); ok {
		return writer
	}
	if f, ok := w.(*File); ok && f != nil && f.F != nil {
		return f.F
	}
	return nil
}

func (h *IOHost) ReadAll(ctx context.Context, r any) ([]byte, error) {
	reader := getReader(r)
	if reader == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return io.ReadAll(reader)
}

func (h *IOHost) Copy(ctx context.Context, dst, src any) (int64, error) {
	w := getWriter(dst)
	r := getReader(src)
	if w == nil || r == nil {
		return 0, os.ErrInvalid
	}
	return io.Copy(w, r)
}

func (h *IOHost) WriteString(ctx context.Context, w any, s string) (int64, error) {
	writer := getWriter(w)
	if writer == nil {
		return 0, os.ErrInvalid
	}
	n, err := io.WriteString(writer, s)
	return int64(n), err
}

// File 是文件句柄 (包装 *os.File)
// ffigen:methods
type File struct {
	F *os.File
}

// Write 正常工作：宿主读取脚本提供的 []byte 内容
func (f *File) Write(b []byte) (int64, error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	n, err := f.F.Write(b)
	return int64(n), err
}

// WriteAt 正常工作：支持偏移量写入
func (f *File) WriteAt(b []byte, off int64) (int64, error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	n, err := f.F.WriteAt(b, off)
	return int64(n), err
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	return f.F.Seek(offset, whence)
}

func (f *File) Close() error {
	if f == nil || f.F == nil {
		return nil
	}
	return f.F.Close()
}

func (f *File) Sync() error {
	if f == nil || f.F == nil {
		return os.ErrInvalid
	}
	return f.F.Sync()
}

func (f *File) Truncate(size int64) error {
	if f == nil || f.F == nil {
		return os.ErrInvalid
	}
	return f.F.Truncate(size)
}

func (f *File) WriteString(s string) (int64, error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	n, err := f.F.WriteString(s)
	return int64(n), err
}

func (f *File) Name() string {
	if f == nil || f.F == nil {
		return ""
	}
	return f.F.Name()
}

// 满足 io.Writer 接口，供宿主侧其他库使用
func (f *File) WriteNative(p []byte) (n int, err error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	return f.F.Write(p)
}

// RegisterIOAll 为方便外部调用提供的注册函数
func RegisterIOAll(executor interface {
	RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string)
	RegisterStructSpec(string, ast.GoMiniType)
	RegisterConstant(string, string)
}, impl IO, registry *ffigo.HandleRegistry,
) {
	RegisterIO(executor, impl, registry)
	RegisterFile(executor, registry)
}
