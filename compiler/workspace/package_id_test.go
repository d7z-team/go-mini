package workspace

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
)

func TestSourceTreesAssignStructuredPackageIdentity(t *testing.T) {
	module, err := NewTreeSourceSet("example.com/app", []TreeFile{
		{Path: "main.mgo", Text: "package main\n"},
		{Path: "lib/lib.mgo", Text: "package lib\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := module.Package("example.com/app")
	dependency, _, _ := module.Package("example.com/app/lib")
	if root.ID.String() != "module:example.com/app::" || dependency.ID.String() != "module:example.com/app::lib" {
		t.Fatalf("module identities = %q, %q", root.ID, dependency.ID)
	}
	standard, err := NewPackageTreeSourceSet([]TreeFile{{Path: "fmt/fmt.mgo", Text: "package fmt\n"}})
	if err != nil {
		t.Fatal(err)
	}
	fmtPackage, _, _ := standard.Package("fmt")
	if fmtPackage.ID.String() != "std::fmt" {
		t.Fatalf("stdlib identity = %q", fmtPackage.ID)
	}
	memory, err := NewMemorySourceSet([]SourcePackage{{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	fallback, _, _ := memory.Package("example")
	if fallback.ID.String() != "module:example::" {
		t.Fatalf("fallback identity = %q", fallback.ID)
	}
}
