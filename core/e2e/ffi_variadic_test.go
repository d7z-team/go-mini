package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// PrinterAPI 演示变长参数
//
// ffigen:module printer
type PrinterAPI interface {
	Log(prefix string, args ...any) error
	Println(args ...any)
}

type MockPrinter struct {
	LastOutput string
}

func (m *MockPrinter) Println(args ...any) {
	var parts []string
	for _, arg := range args {
		parts = append(parts, fmt.Sprint(arg))
	}
	m.LastOutput = strings.Join(parts, " ")
}

func (m *MockPrinter) Log(prefix string, args ...any) error {
	var parts []string
	parts = append(parts, prefix)
	for _, arg := range args {
		parts = append(parts, fmt.Sprint(arg))
	}
	m.LastOutput = strings.Join(parts, ":")
	return nil
}

// 模拟 ffigen 生成的注册逻辑
func RegisterPrinter(executor *engine.MiniExecutor, impl PrinterAPI) {
	bridge := &PrinterBridge{impl: impl}
	// 注意：FFI spec 中的变长参数由 ... 前缀标识
	executor.RegisterFFI("printer.Log", bridge, 1, "function(String, ...Any) Result<Void>", "Log with variadic args")
}

type PrinterBridge struct {
	impl PrinterAPI
}

func (b *PrinterBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	reader := ffigo.NewReader(args)
	prefix := reader.ReadString()

	// 根据协议：[Count (Uint32)] [Item1] [Item2]...
	count := int(reader.ReadUint32())
	variadic := make([]any, count)
	for i := 0; i < count; i++ {
		variadic[i] = reader.ReadAny()
	}

	err := b.impl.Log(prefix, variadic...)

	resBuf := ffigo.GetBuffer()
	if err != nil {
		resBuf.WriteByte(1)
		resBuf.WriteString(err.Error())
	} else {
		resBuf.WriteByte(0)
	}
	return resBuf.Bytes(), nil
}

func (b *PrinterBridge) DestroyHandle(uint32) error { return nil }

func TestFFIVariadic(t *testing.T) {
	executor := engine.NewMiniExecutor()
	printer := &MockPrinter{}
	RegisterPrinter(executor, printer)

	code := `
	package main
	import "printer"
	func main() {
		printer.Log("INFO", "User", 123, true)
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

	expected := "INFO:User:123:true"
	if printer.LastOutput != expected {
		t.Errorf("Expected %q, got %q", expected, printer.LastOutput)
	}
}
