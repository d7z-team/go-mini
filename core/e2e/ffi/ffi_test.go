package tests

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// MockFmtBridge 模拟 fmt 包的 Bridge
type MockFmtBridge struct {
	LastOutput string
}

func (b *MockFmtBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.MethodID == 1 { // Println
		reader := ffigo.NewReader(req.Args)
		msg := reader.ReadString()
		b.LastOutput = msg
		fmt.Printf("[FFI fmt.Println] %s\n", msg) //nolint:forbidigo // allowed for testing
		return nil, nil
	}
	return nil, fmt.Errorf("unknown method %d", req.MethodID)
}

func (b *MockFmtBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}

func (b *MockFmtBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestFFIPrintln(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &MockFmtBridge{}

	// 注册 FFI 路由
	executor.RegisterFFISchema("sandbox.Println", bridge, 1, runtime.MustParseRuntimeFuncSig("function(String) Void"), "")

	code := `
	package main
	import "sandbox"
	func main() {
		sandbox.Println("Hello from Sandbox!")
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

	if bridge.LastOutput != "Hello from Sandbox!" {
		t.Errorf("FFI output mismatch: got %q, want 'Hello from Sandbox!'", bridge.LastOutput)
	}
}
