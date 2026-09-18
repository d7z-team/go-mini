package compiler_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCheckAnalyzesDependencyExportsWithoutLowering(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: `package lib
const Answer = 42
type Value struct { Count int }
func (v Value) Get() int { return v.Count }
func New() Value { return Value{Count: Answer} }
`}}},
		{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: `package main
import "example/lib"
func main() { value := lib.New(); _ = value.Get(); _ = lib.Answer }
`}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := compiler.Check(compiler.Request{Root: "example/main", Sources: sources})
	if err != nil || !checked.OK() {
		t.Fatalf("Check failed: result=%#v err=%v", checked, err)
	}
	if checked.Stats.PackagesAnalyzed != 2 || checked.Stats.PackagesLowered != 0 || checked.Stats.PackagesCompiled != 0 {
		t.Fatalf("Check pipeline stats = %#v", checked.Stats)
	}
}

func TestPrepareCreatesExecutableClosure(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc unused() int { return 1 }\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiler.Prepare(compiler.Request{Root: "example/main", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Checked.OK() || result.Image == nil {
		t.Fatalf("prepare result = %#v", result)
	}
}

func TestPrepareLibraryRequiresEntry(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/lib",
		Files:      []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Value() int { return 1 }\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiler.Prepare(compiler.Request{Root: "example/lib", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if result.Image != nil || len(result.Checked.Diagnostics) != 1 || result.Checked.Diagnostics[0].Code != "compiler.entry.required" {
		t.Fatalf("prepare result = %#v", result)
	}
}
