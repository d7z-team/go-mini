package e2e

import (
	"context"
	"fmt"
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
		return nil, fmt.Errorf("file not found")
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

func (m *CoverageMockOS) Read(f *File, b []byte) (int, error) {
	copy(b, "hello")
	return 5, nil
}

func (m *CoverageMockOS) Write(f *File, b []byte) (int, error) {
	m.LastBuffer = append([]byte(nil), b...)
	return len(b), nil
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
	bridge := &engine.HandleBridgeWrapper{
		Registry: registry,
		Router: func(reg *ffigo.HandleRegistry, methodID uint32, args []byte) ([]byte, error) {
			return OSHostRouter(mock, reg, methodID, args)
		},
	}

	executor.RegisterFFI("os.Open", bridge, MethodID_OS_Open, "function(String) tuple(TypeHandle, Error)")
	executor.RegisterFFI("os.Read", bridge, MethodID_OS_Read, "function(TypeHandle, TypeBytes) tuple(Int64, Error)")
	executor.RegisterFFI("os.Write", bridge, MethodID_OS_Write, "function(TypeHandle, TypeBytes) tuple(Int64, Error)")

	code := `
	func main() {
		// 1. 测试读写
		h2 := os.Open("test.txt")
		buf := []byte("payload")
		n := os.Write(h2, buf)
		if n != 7 { panic("write length mismatch") }

		readBuf := []byte(".....")
		rn := os.Read(h2, readBuf)
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
	bridge := &engine.HandleBridgeWrapper{
		Registry: ffigo.NewHandleRegistry(),
		Router: func(reg *ffigo.HandleRegistry, methodID uint32, args []byte) ([]byte, error) {
			return OSHostRouter(mock, reg, methodID, args)
		},
	}
	executor.RegisterFFI("os.Open", bridge, MethodID_OS_Open, "function(String) tuple(TypeHandle, Error)")

	code := `func main() { os.Open("missing") }`
	prog, _ := executor.NewRuntimeByGoCode(code)
	err := prog.Execute(context.Background())
	if err == nil || err.Error() != "file not found" {
		t.Fatalf("expected 'file not found' error, got: %v", err)
	}
}
