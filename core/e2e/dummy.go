//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg e2e -out dummy_ffigen.go dummy.go coverage_test.go
package e2e

import "context"

type File struct {
	Name string
}

type FileInfo struct {
	Size uint32
	Name string
}

type Nested struct {
	Info  FileInfo
	Level int
}

type MockOS interface {
	Open(name string) (*File, error)
	Name(f *File) string
	Stat(f *File) (FileInfo, error)
	Read(f *File, b []byte) (int, error)
	Write(f *File, b []byte) (int, error)
	Close(f *File) error
	Deep(n Nested) Nested
}

// ContextMock 验证 Context 传递
//
// ffigen:module ctx_test
type ContextMock interface {
	WithContext(ctx context.Context, key string) string
	WithoutContext(val string) string
}
