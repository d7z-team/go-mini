package oslib

import "context"

type File struct{}

type OS interface {
	Open(ctx context.Context, name string) (*File, error)
	Create(name string) (*File, error) // 没有 context 的例子
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	Remove(name string) error
	Read(f *File, b []byte) (int, error)
	Write(f *File, b []byte) (int, error)
	Close(f *File) error
}
