package iolib

import (
	"context"
	"io"
	"os"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type IOHost struct{}

func (h *IOHost) ReadAll(ctx context.Context, r Reader) ([]byte, error) {
	reader, err := nativeReader(r)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

func (h *IOHost) Copy(ctx context.Context, dst Writer, src Reader) (int64, error) {
	writer, err := nativeWriter(dst)
	if err != nil {
		return 0, err
	}
	reader, err := nativeReader(src)
	if err != nil {
		return 0, err
	}
	return io.Copy(writer, reader)
}

func (h *IOHost) WriteString(ctx context.Context, w Writer, s string) (int64, error) {
	writer, err := nativeWriter(w)
	if err != nil {
		return 0, err
	}
	n, err := io.WriteString(writer, s)
	return int64(n), err
}

func nativeReader(r Reader) (io.Reader, error) {
	if r == nil {
		return nil, io.ErrUnexpectedEOF
	}
	if f, ok := r.(*File); ok {
		if f.F == nil {
			return nil, io.ErrUnexpectedEOF
		}
		return f.F, nil
	}
	return readerAdapter{r: r}, nil
}

func nativeWriter(w Writer) (io.Writer, error) {
	if w == nil {
		return nil, os.ErrInvalid
	}
	if f, ok := w.(*File); ok {
		if f.F == nil {
			return nil, os.ErrInvalid
		}
		return f.F, nil
	}
	return writerAdapter{w: w}, nil
}

type readerAdapter struct {
	r Reader
}

func (a readerAdapter) Read(p []byte) (int, error) {
	ref := &ffigo.BytesRef{Value: p}
	n, err := a.r.Read(ref)
	if len(ref.Value) > len(p) {
		ref.Value = ref.Value[:len(p)]
	}
	copied := copy(p, ref.Value)
	if n < 0 || n > int64(copied) {
		n = int64(copied)
	}
	return int(n), err
}

type writerAdapter struct {
	w Writer
}

func (a writerAdapter) Write(p []byte) (int, error) {
	n, err := a.w.Write(p)
	if n < 0 {
		n = 0
	}
	if n > int64(len(p)) {
		n = int64(len(p))
	}
	return int(n), err
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

// Read 通过 BytesRef 将读取结果整体回写给 VM，匹配 io.Reader 的 n, err 语义。
func (f *File) Read(buf *ffigo.BytesRef) (int64, error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	if buf == nil {
		return 0, os.ErrInvalid
	}
	n, err := f.F.Read(buf.Value)
	if n >= 0 && n <= len(buf.Value) {
		buf.Value = buf.Value[:n]
	}
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

// ReadAt 通过 BytesRef 将读取结果整体回写给 VM，匹配 io.ReaderAt 的 n, err 语义。
func (f *File) ReadAt(buf *ffigo.BytesRef, off int64) (int64, error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	if buf == nil {
		return 0, os.ErrInvalid
	}
	n, err := f.F.ReadAt(buf.Value, off)
	if n >= 0 && n <= len(buf.Value) {
		buf.Value = buf.Value[:n]
	}
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

// WriteNative 提供原生 int 返回值的写入入口，便于宿主侧适配标准 io.Writer。
func (f *File) WriteNative(p []byte) (n int, err error) {
	if f == nil || f.F == nil {
		return 0, os.ErrInvalid
	}
	return f.F.Write(p)
}
