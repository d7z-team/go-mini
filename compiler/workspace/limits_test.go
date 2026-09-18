package workspace

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
)

func TestParsePackageAppliesCombinedASTNodeLimit(t *testing.T) {
	_, diagnostics, err := ParsePackageWithLimits(SourcePackage{
		ModulePath: "example/data",
		Files: []source.File{
			{Path: "a.mgo", Text: "package data\nvar A = 1\n"},
			{Path: "b.mgo", Text: "package data\nvar B = 2\n"},
		},
	}, Limits{MaxASTNodes: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "ast.limit.nodes" {
			return
		}
	}
	t.Fatalf("missing package AST node limit diagnostic: %#v", diagnostics)
}
