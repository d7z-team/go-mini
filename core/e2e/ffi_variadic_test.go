package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type PrinterAPI interface {
	Println(args ...any)
}

type MockPrinter struct {
	LastMsg string
}

func (m *MockPrinter) Println(args ...any) {
	var parts []string
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%v", arg))
	}
	m.LastMsg = strings.Join(parts, " ")
}

type MockPrinterBridge struct {
	impl     *MockPrinter
	registry *ffigo.HandleRegistry
}

func (b *MockPrinterBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return PrinterAPIHostRouter(ctx, b.impl, b.registry, methodID, args)
}

func (b *MockPrinterBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestFFIVariadic(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &MockPrinterBridge{
		impl:     &MockPrinter{},
		registry: ffigo.NewHandleRegistry(),
	}

	executor.RegisterFFI("fmt.Println", bridge, MethodID_PrinterAPI_Println, "function(...Any) Void")

	code := `
	package main
	import "fmt"

	func main() {
		fmt.Println("FFI", "is", "working", 123, true)
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

	expected := "FFI is working 123 true"
	if bridge.impl.LastMsg != expected {
		t.Fatalf("expected %q, got %q", expected, bridge.impl.LastMsg)
	}
}
