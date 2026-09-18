package minigo_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestSourceSnapshotsPreserveCacheAndRunningProgram(t *testing.T) {
	root := t.TempDir()
	dependency := t.TempDir()
	files := map[string]string{
		"main.mgo":  "package app\nimport \"example.com/dep\"\nfunc Value() int { return dep.Value() }\n",
		"value.mgo": "package dep\nfunc Value() int { return 42 }\n",
	}
	for name, data := range files {
		directory := root
		if name == "value.mgo" {
			directory = dependency
		}
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	backend := cache.NewMemoryBackend()
	compile := func() (*minigoruntime.Program, minigo.Result) {
		t.Helper()
		directory, err := workspace.LoadSources(t.Context(), root, "example.com/app", []workspace.DirectorySource{{Module: "example.com/dep", Directory: dependency}})
		if err != nil {
			t.Fatal(err)
		}
		sources := directory.Sources
		engine, err := minigo.New(minigo.Config{Sources: sources, Cache: backend, Symbols: true})
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		program, result, err := engine.Compile("example.com/app", minigo.EntryPoint{Name: "value", Function: "Value"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: %v, diagnostics=%v", err, result.Diagnostics)
		}
		return program, result
	}
	first, _ := compile()
	warm, result := compile()
	if first.Hash() != warm.Hash() || first.SymbolsHash() == "" || first.SymbolsHash() != warm.SymbolsHash() || result.Stats.PrepareCacheHits != 1 {
		t.Fatalf("identical snapshot did not reuse code and symbols: stats=%+v", result.Stats)
	}
	instance, err := first.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := os.WriteFile(filepath.Join(dependency, "value.mgo"), []byte("package dep\nfunc Value() int { return 43 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, result := compile()
	if changed.Hash() == first.Hash() || result.Stats.PrepareCacheHits != 0 {
		t.Fatalf("local replacement change reused old image: stats=%+v", result.Stats)
	}
	run, err := instance.Call(context.Background(), "value")
	if err != nil || len(run.Values) != 1 {
		t.Fatalf("old instance call: %+v, %v", run, err)
	}
	if value, ok := run.Values[0].Int64(); !ok || value != 42 {
		t.Fatalf("source refresh changed running code: %+v", run)
	}
	updated, err := changed.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer updated.Close()
	run, err = updated.Call(context.Background(), "value")
	if err != nil || len(run.Values) != 1 {
		t.Fatalf("updated instance call: %+v, %v", run, err)
	}
	if value, ok := run.Values[0].Int64(); !ok || value != 43 {
		t.Fatalf("updated source was not executed: %+v", run)
	}
}
