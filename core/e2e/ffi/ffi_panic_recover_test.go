package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestFFIPanicCanBeRecoveredInScript(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	bridge := &panicInterceptBridge{}
	testsurface.UseRoute(t, executor, "sandbox.CallBoom", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

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
	executor := engine.MustNewMiniExecutor()
	bridge := &panicInterceptBridge{}
	testsurface.UseRoute(t, executor, "sandbox.InvokeBoom", bridge, 0, runtime.MustParseRuntimeFuncSig("function() Void"), "")

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
	executor := engine.MustNewMiniExecutor()
	bridge := &panicInterceptBridge{}
	testsurface.UseRoute(t, executor, "sandbox.CallBoom", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

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
