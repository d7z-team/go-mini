package workspace

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

func TestOverlayPreservesOriginAndUnselectedCandidates(t *testing.T) {
	selected, diagnostics, err := SelectPackage(SourcePackage{ModulePath: "sample", Files: []source.File{
		{Path: "main.mgo", OriginPath: "original/main.go", Text: "package sample\nfunc Answer() int { return 1 }"},
		{Path: "optional.mgo", Text: "//go:build optional\n\npackage sample\n"},
	}}, target.Target{})
	if err != nil || source.HasErrors(diagnostics) {
		t.Fatalf("select: %v %v", err, diagnostics)
	}
	base, err := NewMemorySourceSet([]SourcePackage{selected})
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := Overlay(base, []SourceChange{{ModulePath: "sample", Path: "main.mgo", Text: "package sample\nfunc Answer() int { return 2 }"}})
	if err != nil {
		t.Fatal(err)
	}
	changed, _, err := overlay.Package("sample")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Files[0].OriginPath != "original/main.go" {
		t.Fatalf("origin: %q", changed.Files[0].OriginPath)
	}
	before := map[string]SourceCandidate{}
	for _, candidate := range selected.SourceCandidates {
		before[candidate.Path] = candidate
	}
	after := map[string]SourceCandidate{}
	for _, candidate := range changed.SourceCandidates {
		after[candidate.Path] = candidate
	}
	if after["optional.mgo"] != before["optional.mgo"] {
		t.Fatal("unselected source identity changed")
	}
	if after["main.mgo"].Hash == before["main.mgo"].Hash {
		t.Fatal("updated source retained old identity")
	}
	original, _, _ := base.Package("sample")
	if original.Files[0].Text != selected.Files[0].Text {
		t.Fatal("base snapshot mutated")
	}
}
