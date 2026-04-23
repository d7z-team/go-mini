package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type panicInterceptBridge struct{}

func (b *panicInterceptBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	switch methodID {
	case 1:
		panic("bridge-call-boom")
	default:
		return nil, fmt.Errorf("unexpected method id %d", methodID)
	}
}

func (b *panicInterceptBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	switch method {
	case "InvokeBoom", "sandbox.InvokeBoom":
		panic("bridge-invoke-boom")
	default:
		return nil, fmt.Errorf("unexpected method %s", method)
	}
}

func (b *panicInterceptBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestFFIPanicCanBeRecoveredInScript(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &panicInterceptBridge{}
	executor.RegisterFFISchema("sandbox.CallBoom", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "sandbox"

func main() {
	defer func() {
		r := recover()
		if r == nil {
			panic("expected recover value")
		}
		if string(r) != "FFI panic: bridge-call-boom" {
			panic("unexpected recover: " + string(r))
		}
	}()

	sandbox.CallBoom()
	panic("unreachable")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestFFIPanicInvokeRouteCanBeRecoveredInScript(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &panicInterceptBridge{}
	executor.RegisterFFISchema("sandbox.InvokeBoom", bridge, 0, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "sandbox"

func main() {
	defer func() {
		r := recover()
		if r == nil {
			panic("expected recover value")
		}
		if string(r) != "FFI panic: bridge-invoke-boom" {
			panic("unexpected recover: " + string(r))
		}
	}()

	sandbox.InvokeBoom()
	panic("unreachable")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestFFIPanicBubblesWhenUnhandled(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &panicInterceptBridge{}
	executor.RegisterFFISchema("sandbox.CallBoom", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "sandbox"

func main() {
	sandbox.CallBoom()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected unhandled ffi panic to fail execution")
	}
	if !strings.Contains(err.Error(), "FFI panic: bridge-call-boom") {
		t.Fatalf("expected ffi panic message, got: %v", err)
	}
}
