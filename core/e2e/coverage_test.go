package e2e

import (
	"context"
	"errors"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// CoverageMockOS 实现了复杂的 OS 接口用于测试覆盖率
type CoverageMockOS struct {
	LastBuffer []byte
}

func (m *CoverageMockOS) Open(name string) (*File, error) {
	if name == "missing" {
		return nil, errors.New("file not found")
	}
	return &File{Name: name}, nil
}

func (m *CoverageMockOS) Name(f *File) string {
	if f == nil {
		return "nil"
	}
	return f.Name
}

func (m *CoverageMockOS) Stat(f *File) (FileInfo, error) {
	return FileInfo{Size: 123, Name: f.Name}, nil
}

func (m *CoverageMockOS) Read(f *File, b []byte) (int64, error) {
	copy(b, "hello")
	return 5, nil
}

func (m *CoverageMockOS) Write(f *File, b []byte) (int64, error) {
	m.LastBuffer = append([]byte(nil), b...)
	return int64(len(b)), nil
}

func (m *CoverageMockOS) Close(f *File) error {
	return nil
}

func (m *CoverageMockOS) Deep(n Nested) Nested {
	n.Level++
	return n
}

func TestFFICoverage(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	mock := &CoverageMockOS{}
	registry := ffigo.NewHandleRegistry()

	RegisterE2EMockOSLibrary(executor, "os", mock, registry)

	code := `
	package main
	import "os"
	import "fmt"

	func main() {
		// 1. 测试读写
		h2, err := os.Open("test.txt")
		if err != nil { panic(err) }

		buf := []byte("payload")
		n, err1 := os.Write(h2, buf)
		if err1 != nil { panic(err1) }
		if n != 7 { panic("write length mismatch") }

		readBuf := []byte(".....")
		rn, err2 := os.Read(h2, readBuf)
		if err2 != nil { panic(err2) }
		if rn != 5 { panic("read length mismatch") }

		// 2. 测试标准库已注入的方法覆盖
		s := fmt.Sprintf("Val: %d", 100)
		if s != "Val: 100" { panic("sprintf mismatch") }
		
		fmt.Println("FFI Coverage Success")
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if string(mock.LastBuffer) != "payload" {
		t.Fatalf("host buffer mismatch: %s", string(mock.LastBuffer))
	}
}

func TestFFIErrorPropagation(t *testing.T) {
	executor := engine.NewMiniExecutor()
	mock := &CoverageMockOS{}
	registry := ffigo.NewHandleRegistry()
	RegisterE2EMockOSLibrary(executor, "os", mock, registry)

	code := `
	package main
	import "os"
	func main() { 
		_, err := os.Open("missing")
		if err != nil {
			panic(err)
		}
	}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	err = prog.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("expected 'file not found' panic, got: %v", err)
	}
}
