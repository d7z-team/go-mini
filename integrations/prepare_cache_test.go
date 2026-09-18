package integrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	miniruntime "github.com/d7z-team/mini-go/runtime"
)

func TestPreparedDependencyChangesMatchFreshExecution(t *testing.T) {
	main, err := os.ReadFile("testdata/prepare_dependency/main.mgo")
	if err != nil {
		t.Fatal(err)
	}
	library, err := os.ReadFile("testdata/prepare_dependency/lib/lib.mgo")
	if err != nil {
		t.Fatal(err)
	}
	backend := cache.NewMemoryBackend()
	var previous string
	for _, edit := range []struct {
		name, source   string
		body, constant int64
	}{
		{"initial", string(library), 1, 1},
		{"body", strings.Replace(string(library), "return 1", "return 2", 1), 2, 1},
		{"constant", strings.Replace(string(library), "Number = 1", "Number = 3", 1), 1, 3},
	} {
		t.Run(edit.name, func(t *testing.T) {
			sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
				{ModulePath: "example/dependency", Files: []source.File{{Path: "main.mgo", Text: string(main)}}},
				{ModulePath: "example/dependency/lib", Files: []source.File{{Path: "lib.mgo", Text: edit.source}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var expected string
			for index, storage := range []cache.Backend{backend, backend, cache.NewMemoryBackend()} {
				result, err := compiler.Prepare(compiler.Request{Root: "example/dependency", Sources: sources, Cache: cache.New(storage), EntryPoints: []compiler.EntryPoint{{Name: "Body", Function: "Body"}, {Name: "Constant", Function: "Constant"}}})
				if err != nil || result.Image == nil || !result.Checked.OK() {
					t.Fatalf("prepare: %v %+v", err, result.Checked.Diagnostics)
				}
				if index == 0 {
					expected = result.Image.Hash
					if expected == previous {
						t.Fatal("changed dependency reused previous image")
					}
					previous = expected
				} else if result.Image.Hash != expected {
					t.Fatal("cached and fresh images differ")
				}
				if index == 1 && result.Checked.Stats.PrepareCacheHits != 1 {
					t.Fatalf("warm cache: %+v", result.Checked.Stats)
				}
				program, err := miniruntime.LoadExecutionImage(*result.Image)
				if err != nil {
					t.Fatal(err)
				}
				requireIntegrationInt(t, callIntegrationEntry(t, program, "Body"), edit.body)
				requireIntegrationInt(t, callIntegrationEntry(t, program, "Constant"), edit.constant)
			}
		})
	}
}
