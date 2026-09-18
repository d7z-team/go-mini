package integrations_test

import (
	"os"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestConstantTargetsAcrossOptimizationAndCache(t *testing.T) {
	sources, err := workspace.DiscoverIndexedSourceTree(os.DirFS("testdata/constant_targets"), ".", "example/constants")
	if err != nil {
		t.Fatal(err)
	}
	backend := cache.NewDiskBackend(t.TempDir())
	for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
		var imageHash string
		for pass := range 2 {
			session, err := compiler.New(compiler.Options{Sources: sources, Cache: cache.New(backend), Optimization: level})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Prepare("example/constants", []compiler.EntryPoint{{Name: "main", ModulePath: "example/constants", Function: "Main"}})
			session.Close()
			if err != nil || result.Image == nil || !result.Checked.OK() {
				t.Fatalf("O%d pass %d: %v %+v", level, pass, err, result.Checked.Diagnostics)
			}
			if pass == 1 && imageHash != result.Image.Hash {
				t.Fatal("warm cache changed constant image")
			}
			imageHash = result.Image.Hash
			program, err := minigoruntime.LoadExecutionImage(*result.Image)
			if err != nil {
				t.Fatal(err)
			}
			requireIntegrationInt(t, callIntegrationEntry(t, program, "main"), 255)
		}
	}
}
