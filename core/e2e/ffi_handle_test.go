package e2e

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type MockOS struct {
	files map[string]*File
}

func (m *MockOS) Open(name string) (*File, error) {
	if name == "error.txt" {
		return nil, fmt.Errorf("file not found")
	}
	f := &File{Name: name}
	return f, nil
}

func (m *MockOS) Name(f *File) string {
	return f.Name
}

func (m *MockOS) Stat(f *File) (FileInfo, error) {
	if f == nil {
		return FileInfo{}, fmt.Errorf("nil file")
	}
	return FileInfo{Size: 1024, Name: f.Name}, nil
}

func (m *MockOS) Close(f *File) error {
	if f == nil {
		return fmt.Errorf("nil file")
	}
	f.Name = "closed"
	return nil
}

type MockOSBridge struct {
	impl     *MockOS
	registry *ffigo.HandleRegistry
}

func (b *MockOSBridge) Call(methodID uint32, args []byte) ([]byte, error) {
	return OSHostRouter(b.impl, b.registry, methodID, args)
}

func (b *MockOSBridge) DestroyHandle(handle uint32) error {
	b.registry.Remove(handle)
	return nil
}

func TestFFIHandle(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &MockOSBridge{
		impl:     &MockOS{},
		registry: ffigo.NewHandleRegistry(),
	}

	executor.RegisterFFI("os.Open", bridge, MethodID_OS_Open, "function(String) tuple(TypeHandle, Error)")
	executor.RegisterFFI("os.Name", bridge, MethodID_OS_Name, "function(TypeHandle) String")
	executor.RegisterFFI("os.Stat", bridge, MethodID_OS_Stat, "function(TypeHandle) tuple(FileInfo, Error)")
	executor.RegisterFFI("os.Close", bridge, MethodID_OS_Close, "function(TypeHandle) Error")

	code := `
	func main() {
		file := os.Open("test.txt")
		
		name := os.Name(file)
		if name != "test.txt" {
			panic("wrong name")
		}

		os.Close(file)
		
		nameAfterClose := os.Name(file)
		if nameAfterClose != "closed" {
			panic("close failed")
		}
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
}
