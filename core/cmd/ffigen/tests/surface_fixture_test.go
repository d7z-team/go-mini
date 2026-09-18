//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg tests -out surface_fixture_ffigen_test.go surface_fixture_test.go coverage_test.go
package tests

import "context"

type File struct {
	Name string
}

type FileInfo struct {
	Size int64
	Name string
}

type Nested struct {
	Info  FileInfo
	Level int64
}

// ffigen:module os
type MockOS interface {
	Open(name string) (*File, error)
	Name(f *File) string
	Stat(f *File) (FileInfo, error)
	Read(f *File, b []byte) (int64, error)
	Write(f *File, b []byte) (int64, error)
	Close(f *File) error
	Deep(n Nested) Nested
}

// ffigen:module ctx_test
type ContextMock interface {
	WithContext(ctx context.Context, key string) string
	WithoutContext(val string) string
}

type NativeStruct struct {
	Value int64
	Msg   string
}

type NativeHandle struct {
	Value int64
	Msg   string
}

// ffigen:module native
type NativeMock interface {
	GetStruct() NativeStruct
	GetPtr() *NativeHandle
	SetStruct(s NativeStruct) int64
	SetPtr(s *NativeHandle) int64
}

type Selector struct {
	Value int64
}

// ffigen:module browser
// ffigen:methods Page
type Page struct {
	Value int64
}

func (p *Page) GetByPlaceholder(text string, exact ...bool) *Selector {
	_ = text
	_ = exact
	return &Selector{Value: 1}
}
