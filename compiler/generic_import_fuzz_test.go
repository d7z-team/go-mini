package compiler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/runtime/bytecode"
)

func FuzzGenericImportCache(f *testing.F) {
	f.Add(int16(3), false)
	f.Add(int16(-7), true)
	f.Fuzz(func(t *testing.T, value int16, reverse bool) {
		declaration := "rows :="
		if reverse {
			declaration = "var rows ="
		}
		files := []source.File{
			{Path: "a.mgo", Text: "package main\nimport \"example/lib\"\nfunc Result() int { return Local(lib.Value()) + Imported() }"},
			{Path: "b.mgo", Text: fmt.Sprintf("package main\nimport . \"example/lib\"\nfunc Local[T any](v T) int { return Value() }\nfunc Imported() int { %s Wrap([]int{%d}); return Identity(rows[0])[0] }", declaration, value)},
		}
		if reverse {
			files[0], files[1] = files[1], files[0]
		}
		packages := []workspace.SourcePackage{
			{ModulePath: "example/main", Files: files},
			{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Value() int { return 3 }\nfunc Identity[T any](v T) T { return v }\nfunc Wrap[T any](v T) []T { return []T{v} }"}}},
		}
		sources, err := workspace.NewMemorySourceSet(packages)
		if err != nil {
			t.Fatal(err)
		}
		request := compiler.Request{Root: "example/main", Sources: sources, Cache: cache.New(cache.NewMemoryBackend()), Symbols: true}
		checked, err := compiler.Check(request)
		if err != nil || !checked.OK() {
			t.Fatalf("check: %v %v", err, checked.Diagnostics)
		}
		cold, err := compiler.Compile(request)
		if err != nil || !cold.OK() {
			t.Fatalf("cold: %v %v", err, cold.Diagnostics)
		}
		warm, err := compiler.Compile(request)
		if err != nil || !warm.OK() || warm.Stats.PackagesCompiled != 0 || warm.Stats.PackageCacheHits == 0 {
			t.Fatalf("warm: %v %v %+v", err, warm.Diagnostics, warm.Stats)
		}
		for path, artifact := range cold.Artifacts {
			cached := warm.Artifacts[path]
			freshHash, freshErr := bytecode.Hash(&artifact)
			cachedHash, cachedErr := bytecode.Hash(&cached)
			if freshErr != nil || cachedErr != nil || freshHash != cachedHash {
				t.Fatalf("artifact %s: fresh=%s (%v), cached=%s (%v)", path, freshHash, freshErr, cachedHash, cachedErr)
			}
		}
		freshSymbols, err := json.Marshal(cold.PackageSymbols)
		if err != nil {
			t.Fatal(err)
		}
		cachedSymbols, err := json.Marshal(warm.PackageSymbols)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(freshSymbols, cachedSymbols) || !reflect.DeepEqual(cold.ExportHashes, warm.ExportHashes) {
			t.Fatal("cached generics changed artifacts, symbols or source exports")
		}
		// A changed caller must instantiate against the cached dependency's
		// source signatures, rather than relying on a retained checked AST.
		files[0].Text += "\nfunc Extra() {}\n"
		changed, err := workspace.NewMemorySourceSet(packages)
		if err != nil {
			t.Fatal(err)
		}
		request.Sources = changed
		rebuilt, err := compiler.Compile(request)
		if err != nil || !rebuilt.OK() || rebuilt.Stats.PackageCacheHits == 0 || rebuilt.Stats.PackagesCompiled == 0 {
			t.Fatalf("cached dependency: %v %v %+v", err, rebuilt.Diagnostics, rebuilt.Stats)
		}
	})
}
