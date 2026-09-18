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
	executor := engine.MustNewMiniExecutor()
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

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "usesrun"

func main() {
	if !usesrun.Value() {
		panic("module import did not observe run controller")
	}
}
`)
	if err != nil {
		t.Fatalf("compile importer failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute importer failed: %v", err)
	}
}
