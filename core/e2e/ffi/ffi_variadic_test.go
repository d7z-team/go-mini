package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

// PrinterAPI 演示变长参数

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
func RegisterPrinter(t *testing.T, executor *engine.MiniExecutor, impl PrinterAPI) {
	t.Helper()
	bridge := &PrinterBridge{impl: impl}
	testsurface.UseRoute(t, executor, "printer.Log", bridge, 1, runtime.MustParseRuntimeFuncSig("function(String, ...Any) tuple(Void, String)"), "Log with variadic args")
}

type PrinterBridge struct {
	impl PrinterAPI
}

func (b *PrinterBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	prefix, err := reader.ReadString()
	if err != nil {
		return nil, err
	}

	// 根据标准化协议：[Count (Uvarint)] [Item1] [Item2]...
	rawCount, err := reader.ReadUvarint()
	if err != nil {
		return nil, err
	}
	count := int(rawCount)
	variadic := make([]any, count)
	for i := 0; i < count; i++ {
		variadic[i], err = reader.ReadAny()
		if err != nil {
			return nil, err
		}
	}

	err = b.impl.Log(prefix, variadic...)

	resBuf := ffigo.GetBuffer()
	if err != nil {
		resBuf.WriteRawError(err.Error(), 0)
	} else {
		resBuf.WriteRawError("", 0)
	}
	return resBuf.Bytes(), nil
}

func (b *PrinterBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}

func (b *PrinterBridge) DestroyHandle(uint32) error { return nil }

func TestFFIVariadic(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	printer := &MockPrinter{}
	RegisterPrinter(t, executor, printer)

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
