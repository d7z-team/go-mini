package integrations_test

import (
	"os"
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestIntegrationMethodsAndConversions(t *testing.T) {
	sources, err := workspace.DiscoverIndexedSourceTree(os.DirFS("testdata/execution/type_conversion"), ".", "example")
	if err != nil {
		t.Fatal(err)
	}
	root, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []compiler.OptimizationLevel{compiler.OptimizationNone, compiler.OptimizationDefault, compiler.OptimizationFull} {
		engine, err := minigo.New(minigo.Config{Sources: sources, Cache: cache.NewDiskBackend(root), Optimization: level, Symbols: true})
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		checked, err := engine.Check("example")
		if err != nil || !checked.OK() {
			t.Fatalf("check: %v %+v", err, checked)
		}
		program, result, err := engine.Compile("example", minigo.EntryPoint{Name: "result", Function: "Result"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: %v %+v", err, result)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), 42)
	}
}
