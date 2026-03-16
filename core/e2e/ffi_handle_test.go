package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type MockOS struct {
	Files map[string]string
}

func (m *MockOS) Open(name string) (*File, error) {
	return &File{Name: name}, nil
}

func (m *MockOS) Name(f *File) string {
	if f == nil {
		return "nil"
	}
	return f.Name
}

func (m *MockOS) Stat(f *File) (FileInfo, error) {
	return FileInfo{Size: 100, Name: f.Name}, nil
}

func (m *MockOS) Read(f *File, b []byte) (int, error) {
	return 0, nil
}

func (m *MockOS) Write(f *File, b []byte) (int, error) {
	return 0, nil
}

func (m *MockOS) Close(f *File) error {
	f.Name = "closed"
	return nil
}

func (m *MockOS) Deep(n Nested) Nested {
	return n
}

type MockOSBridge struct {
	impl     *MockOS
	registry *ffigo.HandleRegistry
}

func (b *MockOSBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return OSHostRouter(ctx, b.impl, b.registry, methodID, args)
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

	executor.RegisterFFI("os.Open", bridge, MethodID_OS_Open, "function(String) Result<TypeHandle>")
	executor.RegisterFFI("os.Name", bridge, MethodID_OS_Name, "function(TypeHandle) String")
	executor.RegisterFFI("os.Stat", bridge, MethodID_OS_Stat, "function(TypeHandle) Result<FileInfo>")
	executor.RegisterFFI("os.Close", bridge, MethodID_OS_Close, "function(TypeHandle) Result<Void>")

	code := `
	package main
	import "os"

	func main() {
		res := os.Open("test.txt")
		if res.err != nil { panic(res.err) }
		file := res.val
		
		name := os.Name(file)
		if name != "test.txt" {
			panic("wrong name")
		}

		resC := os.Close(file)
		if resC.err != nil { panic("close failed") }
		
		nameAfterClose := os.Name(file)
		if nameAfterClose != "closed" {
			panic("close failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 4. Test Proxy Direct Use
	proxy := &OSProxy{bridge: bridge, registry: bridge.registry}
	ctx := context.Background()
	
	f, err := proxy.Open(ctx, "proxy.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "proxy.txt" {
		t.Fatal("wrong name")
	}

	name := proxy.Name(ctx, f)
	if name != "proxy.txt" {
		t.Fatal("wrong name")
	}

	err = proxy.Close(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Name(ctx, f) != "closed" {
		t.Fatal("close failed")
	}
}
