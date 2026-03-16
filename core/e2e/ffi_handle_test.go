package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type HandleMockOS struct {
	Files map[string]string
}

func (m *HandleMockOS) Open(name string) (*File, error) {
	return &File{Name: name}, nil
}

func (m *HandleMockOS) Name(f *File) string {
	if f == nil {
		return "nil"
	}
	return f.Name
}

func (m *HandleMockOS) Stat(f *File) (FileInfo, error) {
	return FileInfo{Size: 100, Name: f.Name}, nil
}

func (m *HandleMockOS) Read(f *File, b []byte) (int, error) {
	return 0, nil
}

func (m *HandleMockOS) Write(f *File, b []byte) (int, error) {
	return 0, nil
}

func (m *HandleMockOS) Close(f *File) error {
	f.Name = "closed"
	return nil
}

func (m *HandleMockOS) Deep(n Nested) Nested {
	return n
}

func TestFFIHandle(t *testing.T) {
	executor := engine.NewMiniExecutor()
	mock := &HandleMockOS{}
	registry := ffigo.NewHandleRegistry()

	RegisterE2EMockOSLibrary(executor, "os", mock, registry)

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
	bridge := &MockOS_Bridge{Impl: mock, Registry: registry}
	proxy := &MockOSProxy{bridge: bridge, registry: registry}
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
