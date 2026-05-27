package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

type importRunControllerBridge struct{}

func (importRunControllerBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.MethodID != 1 {
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteBool(runtime.RunControllerFromContext(ctx) != nil)
	return buf.Bytes(), nil
}

func (importRunControllerBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (importRunControllerBridge) DestroyHandle(uint32) error {
	return nil
}

func TestExecutorImportModulePathRunsWithRunController(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "host.HasRunController", importRunControllerBridge{}, 1, runtime.MustParseRuntimeFuncSig("function() Bool"), "")
	if err := executor.UseSurface(surface.Library("usesrun", surface.GoFile("usesrun.mgo", `
package usesrun

import "host"

var OK = host.HasRunController()

func Value() bool {
	return OK
}
`))); err != nil {
		t.Fatalf("UseSurface library failed: %v", err)
	}

	module, err := executor.Import(context.Background(), "usesrun")
	if err != nil {
		t.Fatalf("Import usesrun failed: %v", err)
	}
	results, err := executor.Eval(context.Background(), "mod.Value()", map[string]interface{}{"mod": module})
	if err != nil {
		t.Fatalf("Eval imported module failed: %v", err)
	}
	if len(results) != 1 || results[0] == nil || !results[0].Bool {
		t.Fatalf("imported module did not observe run controller: %#v", results)
	}
}
