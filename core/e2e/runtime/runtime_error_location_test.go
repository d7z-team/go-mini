package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestNilFunctionCallErrorCarriesSourceLine(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `package main
func main() {
	var f func()
	f()
}`

	prog, err := executor.NewRuntimeByGoFile("nil_call_test.mgo", code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected nil function call to fail")
	}

	var vmErr *runtime.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected *runtime.VMError, got (%T) %v", err, err)
	}
	if len(vmErr.Frames) == 0 {
		t.Fatalf("expected stack frames, got %#v", vmErr)
	}
	if vmErr.Frames[0].Filename != "nil_call_test.mgo" {
		t.Fatalf("expected frame filename nil_call_test.mgo, got %q", vmErr.Frames[0].Filename)
	}
	if vmErr.Frames[0].Line != 4 {
		t.Fatalf("expected nil call at line 4, got frame %+v", vmErr.Frames[0])
	}
	if !strings.Contains(err.Error(), "nil_call_test.mgo:4:2") {
		t.Fatalf("expected formatted error to contain source location, got: %v", err)
	}
}
