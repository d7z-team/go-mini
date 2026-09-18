package runtime_test

import (
	"context"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/ffi"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/stdlib"
)

func TestCompiledFFICallUsesOpaqueBridge(t *testing.T) {
	application, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/ffi",
		Files: []source.File{{Path: "main.mgo", Text: `package main
import (
	"ffi"
	"errors"
)
func Call() (string, error) {
	value, err := ffi.Call("echo", []byte("hello"))
	return string(value), err
}
func Missing() bool {
	_, err := ffi.Call("missing", nil)
	return errors.Is(err, ffi.ErrRouteUnavailable)
}
`}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	standard, err := workspace.StandardLibrary(stdlib.Open())
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.MergeSourceSets(application, standard)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := compiler.Prepare(compiler.Request{
		Root: "example/ffi", Sources: sources, Cache: cache.New(cache.NewDiskBackend(cacheRoot)),
		EntryPoints: []compiler.EntryPoint{{Name: "call", Function: "Call"}, {Name: "missing", Function: "Missing"}},
	})
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("prepare FFI program: err=%v diagnostics=%#v", err, prepared.Checked.Diagnostics)
	}
	program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		t.Fatal(err)
	}

	bridge := ffi.CallFunc(func(_ context.Context, request ffi.Request, complete ffi.Completion) (ffi.Call, error) {
		if request.Route != "echo" || string(request.Payload) != "hello" {
			t.Fatalf("request = %#v", request)
		}
		complete(ffi.Result{Payload: []byte("world")})
		return ffi.CancelFunc(func() {}), nil
	})
	instance, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{FFI: bridge})
	if err != nil {
		t.Fatal(err)
	}
	result, err := instance.Call(context.Background(), "call")
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	value, ok := result.Values[0].StringValue()
	if !ok || value != "world" {
		t.Fatalf("result = %#v", result.Values)
	}

	withoutBridge, err := program.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer withoutBridge.Close()
	result, err = withoutBridge.Call(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	missing, _ := result.Values[0].Bool()
	if !missing {
		t.Fatalf("missing route result = %#v", result.Values)
	}
}
