package workspace

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

func TestLoaderReturnsImmutableCachedGraph(t *testing.T) {
	sources, err := NewMemorySourceSet([]SourcePackage{
		{
			ModulePath: "example/main",
			Files:      []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/value\"\nfunc main() {}\n"}},
		},
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "package value\nfunc Value() int { return 1 }\n"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loader, err := NewLoader(sources, target.Target{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := loader.LoadHeadersForRoots([]string{"example/main"})
	if err != nil || len(first.Diagnostics) != 0 {
		t.Fatalf("first graph: diagnostics=%v err=%v", first.Diagnostics, err)
	}
	first.Order[0] = "changed"
	mainHeader := first.Packages["example/main"]
	mainHeader.Package = "changed"
	mainHeader.Source.Files[0].LineStarts[0] = 99
	first.Packages["example/main"] = mainHeader

	second, err := loader.LoadHeadersForRoots([]string{"example/main"})
	if err != nil || len(second.Diagnostics) != 0 {
		t.Fatalf("second graph: diagnostics=%v err=%v", second.Diagnostics, err)
	}
	if len(second.Order) != 2 || second.Order[0] != "example/value" || second.Packages["example/main"].Package != "main" {
		t.Fatalf("cached graph was mutated: %#v", second)
	}
	position, ok := second.Packages["example/main"].Source.Files[0].Position(0)
	if !ok || position.Line != 1 || position.Column != 0 {
		t.Fatalf("cached source metadata was mutated: %+v, ok=%v", position, ok)
	}
}

func TestPackageHeaderCacheSeparatesTargets(t *testing.T) {
	sources, err := NewMemorySourceSet([]SourcePackage{{
		ModulePath: "example/main",
		Files: []source.File{
			{Path: "main.mgo", Text: "package main\nfunc main() {}\n"},
			{Path: "feature.mgo", Text: "//go:build feature\n\npackage main\nimport \"example/feature\"\n"},
		},
	}, {
		ModulePath: "example/feature",
		Files:      []source.File{{Path: "feature.mgo", Text: "package feature\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := LoadHeaders("example/main", sources, target.Target{})
	if err != nil || len(plain.Diagnostics) != 0 {
		t.Fatalf("plain graph: diagnostics=%v err=%v", plain.Diagnostics, err)
	}
	feature, err := LoadHeaders("example/main", sources, target.Target{Tags: []string{"feature"}})
	if err != nil || len(feature.Diagnostics) != 0 {
		t.Fatalf("feature graph: diagnostics=%v err=%v", feature.Diagnostics, err)
	}
	if len(plain.Order) != 1 || len(feature.Order) != 2 || feature.Order[0] != "example/feature" {
		t.Fatalf("target graphs: plain=%v feature=%v", plain.Order, feature.Order)
	}
}

func TestPackageSelectionPreservesStructuredIdentity(t *testing.T) {
	newGraph := func(namespace string) HeaderGraph {
		sources, err := NewMemorySourceSet([]SourcePackage{{
			ID:         PackageID{Namespace: namespace, Path: "main"},
			ModulePath: "example/main",
			Files: []source.File{
				{Path: "main.mgo", Text: "package main\nfunc main() {}\n"},
				{Path: "feature.mgo", Text: "//go:build feature\n\npackage main\n"},
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		graph, err := LoadHeaders("example/main", sources, target.Target{Tags: []string{"feature"}})
		if err != nil || len(graph.Diagnostics) != 0 {
			t.Fatalf("load %q graph: diagnostics=%v err=%v", namespace, graph.Diagnostics, err)
		}
		if got := graph.Packages["example/main"].Source.ID.String(); got != namespace+"::main" {
			t.Fatalf("selected package identity = %q", got)
		}
		return graph
	}

	first := newGraph("workspace:first")
	second := newGraph("workspace:second")
	if first.Hash == second.Hash {
		t.Fatal("distinct package namespaces produced the same header graph hash")
	}
}
