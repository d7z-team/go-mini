package workspace

import (
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
)

func TestOverlayAddsChangesAndDeletesFiles(t *testing.T) {
	base, err := NewMemorySourceSet([]SourcePackage{
		{ModulePath: "example.com/app", Files: []source.File{{Path: "main.mgo", Text: "package main\n"}}, TestFiles: []source.File{{Path: "main_test.mgo", Text: "package main\n"}}},
		{ModulePath: "example.com/app/old", Files: []source.File{{Path: "old/old.mgo", Text: "package old\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := Overlay(base, []SourceChange{
		{ModulePath: "example.com/app", Path: "main.mgo", Text: "package main\nfunc main() {}\n"},
		{ModulePath: "example.com/app", Path: "extra_test.mgo", Text: "package main\n"},
		{ModulePath: "example.com/app/new", Path: "new/new.mgo", Text: "package new\n"},
		{ModulePath: "example.com/app/old", Path: "old/old.mgo", Deleted: true},
	})
	if err != nil {
		t.Fatalf("Overlay failed: %v", err)
	}
	paths, _ := overlay.PackagePaths()
	want := []string{"example.com/app", "example.com/app/new"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("overlay paths = %v, want %v", paths, want)
	}
	pkg, _, _ := overlay.Package("example.com/app")
	if pkg.Files[0].Text != "package main\nfunc main() {}\n" || len(pkg.TestFiles) != 2 {
		t.Fatalf("unexpected overlay package: %#v", pkg)
	}
	basePackage, _, _ := base.Package("example.com/app")
	if basePackage.Files[0].Text != "package main\n" || len(basePackage.TestFiles) != 1 {
		t.Fatalf("base source set was modified: %#v", basePackage)
	}
}

func TestOverlayRejectsDuplicateChange(t *testing.T) {
	base, err := NewMemorySourceSet([]SourcePackage{{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Overlay(base, []SourceChange{
		{ModulePath: "example", Path: "main.mgo", Text: "package main\n"},
		{ModulePath: "example", Path: "main.mgo", Deleted: true},
	})
	if err == nil {
		t.Fatal("Overlay accepted duplicate changes")
	}
}
