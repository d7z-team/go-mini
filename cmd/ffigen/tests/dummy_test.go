//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -out dummy_ffigen_test.go dummy_test.go coverage_test.go
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

// ContextMock 验证 Context 传递

// ffigen:module ctx_test
type ContextMock interface {
	WithContext(ctx context.Context, key string) string
	WithoutContext(val string) string
}

type NativeStruct struct {
	Value int64
	Msg   string
}

// NativeMock 验证原生对象注入

// ffigen:module native
type NativeMock interface {
	GetStruct() NativeStruct
	GetPtr() *NativeStruct
	SetStruct(s NativeStruct) int64
	SetPtr(s *NativeStruct) int64
}

type Selector struct {
	Value int64
}

// ffigen:methods Selector
type VariadicPointerMethods interface {
	GetByPlaceholder(s *Selector, text string, exact ...bool) *Selector
}

// ffigen:methods Page
type Page struct {
	Value int64
}

func (p *Page) GetByPlaceholder(text string, exact ...bool) *Selector {
	_ = text
	_ = exact
	return &Selector{Value: 1}
}
