package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/workspace"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestCompileBuildsReachableDependencyGraph(t *testing.T) {
	packages := []SourcePackage{
		{
			ModulePath: "example/unused",
			Files:      []SourceFile{{Path: "unused.mgo", Text: "package unused\nfunc Value() int { return 0 }\n"}},
		},
		{
			ModulePath: "example/app",
			Files:      []SourceFile{{Path: "app.mgo", Text: "package app\nimport \"example/lib\"\nfunc Main() int { return lib.Value() }\n"}},
		},
		{
			ModulePath: "example/lib",
			Files:      []SourceFile{{Path: "lib.mgo", Text: "package lib\nfunc Value() int { return 42 }\n"}},
		},
	}
	sources, err := workspace.NewMemorySourceSet(packages)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compile(Request{Root: "example/app", Sources: sources})
	if err != nil || !result.OK() {
		t.Fatalf("compile failed: err=%v diagnostics=%#v", err, result.Diagnostics)
	}
	if len(result.Order) != 2 || result.Order[0] != "example/lib" || result.Order[1] != "example/app" {
		t.Fatalf("unexpected dependency-first order: %#v", result.Order)
	}
	if _, ok := result.Artifact("example/unused"); ok {
		t.Fatal("unreachable package was compiled")
	}
	app := result.Artifacts["example/app"]
	dependency := result.Artifacts["example/lib"]
	dependencyHash, err := ir.Hash(&dependency)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Requirements) != 1 || app.Requirements[0].Hash != dependencyHash {
		t.Fatalf("dependency requirement mismatch: %#v", app.Requirements)
	}
}

func TestCompileIsIndependentOfSourceOrdering(t *testing.T) {
	first := []SourcePackage{{
		ModulePath: "example/app",
		Files: []SourceFile{
			{Path: "z.mgo", Text: "package app\nfunc Z() int { return 1 }\n"},
			{Path: "a.mgo", Text: "package app\nfunc A() int { return Z() }\n"},
		},
	}}
	second := []SourcePackage{{ModulePath: first[0].ModulePath, Files: []SourceFile{first[0].Files[1], first[0].Files[0]}}}
	compile := func(packages []SourcePackage) Result {
		t.Helper()
		sources, err := workspace.NewMemorySourceSet(packages)
		if err != nil {
			t.Fatal(err)
		}
		result, err := Compile(Request{Root: "example/app", Sources: sources})
		if err != nil || !result.OK() {
			t.Fatalf("compile failed: %v %#v", err, result.Diagnostics)
		}
		return result
	}
	left := compile(first)
	right := compile(second)
	leftArtifact := left.Artifacts["example/app"]
	leftHash, err := ir.Hash(&leftArtifact)
	if err != nil {
		t.Fatal(err)
	}
	rightArtifact := right.Artifacts["example/app"]
	rightHash, err := ir.Hash(&rightArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if left.GraphHash != right.GraphHash || leftHash != rightHash {
		t.Fatalf("ordering changed output: graph %s/%s artifact %s/%s", left.GraphHash, right.GraphHash, leftHash, rightHash)
	}
}
