package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestFFIPanicCarriesSourceLine(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	bridge := &panicInterceptBridge{}
	testsurface.UseRoute(t, executor, "sandbox.CallBoom", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	code := `package main
import "sandbox"

func main() {
	sandbox.CallBoom()
}`

	prog, err := executor.NewRuntimeByGoFile("ffi_panic_line_test.mgo", code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected unhandled ffi panic to fail execution")
	}

	var vmErr *runtime.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected *runtime.VMError, got (%T) %v", err, err)
	}
	if len(vmErr.Frames) == 0 {
		t.Fatalf("expected stack frames, got %#v", vmErr)
	}
	if vmErr.Frames[0].Filename != "ffi_panic_line_test.mgo" {
		t.Fatalf("expected frame filename ffi_panic_line_test.mgo, got %q", vmErr.Frames[0].Filename)
	}
	if vmErr.Frames[0].Line != 5 {
		t.Fatalf("expected ffi panic call at line 5, got frame %+v", vmErr.Frames[0])
	}
	if !strings.Contains(err.Error(), "ffi_panic_line_test.mgo:5:2") {
		t.Fatalf("expected formatted error to contain source location, got: %v", err)
	}
}
