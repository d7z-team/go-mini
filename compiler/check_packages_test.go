package compiler_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCheckPackagesSharesDependenciesAndReturnsDocuments(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/shared", Files: []source.File{{Path: "shared.mgo", Text: "package shared\nfunc Value() int { return 1 }\n"}}},
		{ModulePath: "example/a", Files: []source.File{{Path: "a.mgo", Text: "package a\nimport \"example/shared\"\nfunc A() int { return shared.Value() }\n"}}},
		{ModulePath: "example/b", Files: []source.File{{Path: "b.mgo", Text: "package b\nimport \"example/shared\"\nfunc B() int { return shared.Value() }\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := compiler.CheckPackages(compiler.Request{Sources: sources}, []string{"example/b", "example/a", "example/a"})
	if err != nil || !checked.OK() {
		t.Fatalf("CheckPackages failed: result=%#v err=%v", checked, err)
	}
	if checked.Stats.PackagesParsed != 3 || checked.Stats.PackagesAnalyzed != 3 {
		t.Fatalf("check stats = %#v", checked.Stats)
	}
	for _, modulePath := range []string{"example/shared", "example/a", "example/b"} {
		if len(checked.Documents[modulePath]) != 1 || checked.ExportHashes[modulePath] == "" {
			t.Fatalf("package facts for %s: documents=%d hash=%q", modulePath, len(checked.Documents[modulePath]), checked.ExportHashes[modulePath])
		}
	}
}
