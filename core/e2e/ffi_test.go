package e2e

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// MockFmtBridge 模拟 fmt 包的 Bridge
type MockFmtBridge struct {
	LastOutput string
}

func (b *MockFmtBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	if methodID == 1 { // Println
		reader := ffigo.NewReader(args)
		msg := reader.ReadString()
		b.LastOutput = msg
		fmt.Printf("[FFI fmt.Println] %s\n", msg) //nolint:forbidigo // allowed for testing
		return nil, nil
	}
	return nil, fmt.Errorf("unknown method %d", methodID)
}

func (b *MockFmtBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestFFIPrintln(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &MockFmtBridge{}

	// 注册 FFI 路由
	executor.RegisterFFI("fmt.Println", bridge, 1, "function(String) Void", "")

	code := `
	package main
	import "fmt"
	func main() {
		fmt.Println("Hello from Sandbox!")
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
