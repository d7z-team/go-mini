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

func (m *MockOS) Read(f *File, b []byte) (int, error) {
	return 0, nil
}

func (m *MockOS) Write(f *File, b []byte) (int, error) {
	return 0, nil
}

func (m *MockOS) Deep(n Nested) Nested {
	return n
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
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestOSProxyDirect(t *testing.T) {
	registry := ffigo.NewHandleRegistry()
	mockOS := &MockOS{}
	bridge := &MockOSBridge{
		impl:     mockOS,
		registry: registry,
	}

	// 初始化 Proxy，并注入 Registry
	proxy := &OSProxy{
		bridge:   bridge,
		registry: registry,
	}

	// 1. 测试返回句柄：Open 会在宿主机创建一个 *File 并注册 ID 返回给 Proxy
	file, err := proxy.Open("direct_test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if file == nil {
		t.Fatal("expected file handle, got nil")
	}

	// 2. 测试发送句柄：Name 会把 file 指针通过 Proxy 重新注册（或识别）并发送给 Host
	name := proxy.Name(file)
	if name != "direct_test.txt" {
		t.Fatalf("expected 'direct_test.txt', got %q", name)
	}

	// 3. 测试句柄生命周期
	err = proxy.Close(file)
	if err != nil {
		t.Fatal(err)
	}

	// 再次获取名称，验证 Host 端对象已被修改
	nameAfter := proxy.Name(file)
	if nameAfter != "closed" {
		t.Fatalf("expected 'closed', got %q", nameAfter)
	}
}
