package workspace

import (
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

func TestLoadHeadersSelectsBuildTagsBeforeParsing(t *testing.T) {
	sources, err := NewMemorySourceSet([]SourcePackage{{
		ModulePath: "example",
		Files: []source.File{
			{Path: "base.mgo", Text: "package example\n"},
			{Path: "debug.mgo", Text: "//go:build debug\n\npackage example\nfunc Mode() int { return 1 }\n"},
			{Path: "release.mgo", Text: "//go:build !debug\n\npackage example\nfunc Mode() int { return 2 }\n"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	debug, err := LoadHeaders("example", sources, target.Target{Tags: []string{"debug"}})
	if err != nil || len(debug.Diagnostics) != 0 {
		t.Fatalf("debug graph failed: %v %#v", err, debug.Diagnostics)
	}
	release, err := LoadHeaders("example", sources, target.Target{})
	if err != nil || len(release.Diagnostics) != 0 {
		t.Fatalf("release graph failed: %v %#v", err, release.Diagnostics)
	}
	paths := func(graph HeaderGraph) []string {
		files := graph.Packages["example"].Source.Files
		out := make([]string, len(files))
		for i := range files {
			out[i] = files[i].Path
		}
		return out
	}
	if got, want := paths(debug), []string{"base.mgo", "debug.mgo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("debug files = %#v, want %#v", got, want)
	}
	if got, want := paths(release), []string{"base.mgo", "release.mgo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("release files = %#v, want %#v", got, want)
	}
	if debug.Hash == release.Hash || len(debug.Packages["example"].Candidates) != 3 {
		t.Fatalf("target did not affect graph identity: debug=%s release=%s", debug.Hash, release.Hash)
	}
}

func TestLoadHeadersReportsBuildConstraintErrors(t *testing.T) {
	sources, err := NewMemorySourceSet([]SourcePackage{{ModulePath: "example", Files: []source.File{{
		Path: "main.mgo", Text: "//go:build debug &&\n\npackage example\n",
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := LoadHeaders("example", sources, target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Diagnostics) != 1 || graph.Diagnostics[0].Code != "compiler.target.constraint" {
		t.Fatalf("diagnostics = %#v", graph.Diagnostics)
	}
}

func TestLoadHeadersTreatsBlankEmbedImportAsCompilerMarker(t *testing.T) {
	sources, err := NewMemorySourceSet([]SourcePackage{{
		ModulePath: "example",
		Files:      []source.File{{Path: "main.mgo", Text: "package example\nimport _ \"embed\"\n//go:embed value.txt\nvar value string\n"}},
		Resources:  []ResourceFile{{Path: "value.txt", Data: []byte("value")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := Load("example", sources, target.Target{})
	if err != nil || len(graph.Diagnostics) != 0 {
		t.Fatalf("Load failed: err=%v diagnostics=%#v", err, graph.Diagnostics)
	}
	if len(graph.Order) != 1 || len(graph.Packages["example"].Imports) != 0 {
		t.Fatalf("embed marker created source dependency: order=%#v imports=%#v", graph.Order, graph.Packages["example"].Imports)
	}
}

func TestParsePackageReportsInvalidEmbed(t *testing.T) {
	pkg := SourcePackage{
		ModulePath: "example/embed",
		Files:      []source.File{{Path: "main.mgo", Text: "package embed\nimport _ \"embed\"\n//go:embed missing.txt\nvar value string\n"}},
		Resources:  []ResourceFile{{Path: "present.txt", Data: []byte("value")}},
	}
	_, diagnostics, err := ParsePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "compiler.embed.pattern" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestParsePackageRequiresEmbedImportInDirectiveFile(t *testing.T) {
	pkg := SourcePackage{
		ModulePath: "example/embed",
		Files: []source.File{
			{Path: "imports.mgo", Text: "package embed\nimport _ \"embed\"\n"},
			{Path: "value.mgo", Text: "package embed\n//go:embed value.txt\nvar value string\n"},
		},
		Resources: []ResourceFile{{Path: "value.txt", Data: []byte("value")}},
	}
	_, diagnostics, err := ParsePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "compiler.embed.import" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestGraphIsReachableAndDeterministic(t *testing.T) {
	files := []TreeFile{
		{Path: "unused/unused.mgo", Text: "package unused\n"},
		{Path: "main.mgo", Text: "package main\nimport \"example.com/project/lib\"\nfunc Main() int { return lib.Value() }\n"},
		{Path: "lib/lib.mgo", Text: "package lib\nfunc Value() int { return 1 }\n"},
		{Path: "main_test.mgo", Text: "package main\nfunc TestMain() {}\n"},
	}
	sources, err := NewTreeSourceSet("example.com/project", files)
	if err != nil {
		t.Fatalf("NewTreeSourceSet failed: %v", err)
	}
	graph, err := Load("example.com/project", sources, target.Target{})
	if err != nil || len(graph.Diagnostics) != 0 {
		t.Fatalf("Load failed: %v %#v", err, graph.Diagnostics)
	}
	if len(graph.Order) != 2 || graph.Order[0] != "example.com/project/lib" || graph.Order[1] != "example.com/project" {
		t.Fatalf("unexpected order: %#v", graph.Order)
	}
	if _, ok := graph.Packages["example.com/project/unused"]; ok {
		t.Fatal("unreachable package entered graph")
	}
	root := graph.Packages[graph.Root]
	if len(root.Source.TestFiles) != 1 || len(root.Program.Files) != 1 {
		t.Fatalf("test classification failed: %#v", root.Source)
	}

	reversed := make([]TreeFile, len(files))
	for i := range files {
		reversed[len(files)-1-i] = files[i]
	}
	secondSources, err := NewTreeSourceSet("example.com/project", reversed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load("example.com/project", secondSources, target.Target{})
	if err != nil || second.Hash != graph.Hash {
		t.Fatalf("graph is not deterministic: %v %q != %q", err, second.Hash, graph.Hash)
	}
}

func TestGraphReportsCycleChain(t *testing.T) {
	sources, err := NewTreeSourceSet("example.com/project", []TreeFile{
		{Path: "a/a.mgo", Text: "package a\nimport \"example.com/project/b\"\n"},
		{Path: "b/b.mgo", Text: "package b\nimport \"example.com/project/a\"\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := Load("example.com/project/a", sources, target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Diagnostics) == 0 || graph.Diagnostics[0].Code != "compiler.workspace.module.cycle" {
		t.Fatalf("missing cycle diagnostic: %#v", graph.Diagnostics)
	}
	if got := graph.Diagnostics[0].Primary; !got.Valid() || got.Start.File != "b/b.mgo" {
		t.Fatalf("cycle diagnostic span = %#v", got)
	}
}

func TestMergeSourceSetsRejectsPackageOwnershipConflict(t *testing.T) {
	left, err := NewTreeSourceSet("example.com/project", []TreeFile{{Path: "main.mgo", Text: "package main\n"}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewTreeSourceSet("example.com/project", []TreeFile{{Path: "other.mgo", Text: "package main\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MergeSourceSets(left, right); err == nil {
		t.Fatal("duplicate package ownership was accepted")
	}
}

func TestTreeSourceSetClassifiesMRPCAsCodegenInput(t *testing.T) {
	sources, err := NewTreeSourceSet("example/app", []TreeFile{
		{Path: "app.mgo", Text: "package app\n"},
		{Path: "service.mrpc", Text: `syntax = "mrpc/v2"; namespace example.app.v1; service API { Ping() returns (); }`},
		{Path: "data.json", Text: `{}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	pkg, ok, err := sources.Package("example/app")
	if err != nil || !ok {
		t.Fatalf("Package failed: ok=%t err=%v", ok, err)
	}
	if len(pkg.Resources) != 2 || pkg.Resources[0].Path != "app.mgo" || pkg.Resources[1].Path != "data.json" {
		t.Fatalf("classified package = %#v", pkg)
	}
}

func TestMatchEmbedResourcesUsesGoDirectoryRules(t *testing.T) {
	resources := []ResourceFile{
		{Path: ".root", Data: []byte("root")},
		{Path: "assets/.hidden", Data: []byte("hidden")},
		{Path: "assets/_private", Data: []byte("private")},
		{Path: "assets/nested/value.txt", Data: []byte("nested")},
		{Path: "assets/public.txt", Data: []byte("public")},
	}
	paths := func(files []ResourceFile) []string {
		out := make([]string, len(files))
		for index := range files {
			out[index] = files[index].Path
		}
		return out
	}
	matched, err := matchEmbedResources([]string{"assets"}, resources)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(matched), []string{"assets/nested/value.txt", "assets/public.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("directory match = %#v, want %#v", got, want)
	}
	matched, err = matchEmbedResources([]string{"assets/*"}, resources)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(matched), []string{"assets/.hidden", "assets/_private", "assets/nested/value.txt", "assets/public.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("glob match = %#v, want %#v", got, want)
	}
	matched, err = matchEmbedResources([]string{"all:assets", ".root"}, resources)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(matched), []string{".root", "assets/.hidden", "assets/_private", "assets/nested/value.txt", "assets/public.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("all match = %#v, want %#v", got, want)
	}
}

func TestTreeSourceSetBuildsPackageRelativeResourceViews(t *testing.T) {
	sources, err := NewTreeSourceSet("example.com/project", []TreeFile{
		{Path: "main.mgo", Text: "package main\n"},
		{Path: "child/child.mgo", Text: "package child\n"},
		{Path: "child/data.txt", Text: "data"},
		{Path: "nested/value.mgo", Text: "package nested\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root, ok, err := sources.Package("example.com/project")
	if err != nil || !ok {
		t.Fatalf("root package: ok=%t err=%v", ok, err)
	}
	if got := resourcePaths(root.Resources); !reflect.DeepEqual(got, []string{"child/child.mgo", "child/data.txt", "main.mgo", "nested/value.mgo"}) {
		t.Fatalf("root resources = %#v", got)
	}
	child, ok, err := sources.Package("example.com/project/child")
	if err != nil || !ok {
		t.Fatalf("child package: ok=%t err=%v", ok, err)
	}
	if got := resourcePaths(child.Resources); !reflect.DeepEqual(got, []string{"child.mgo", "data.txt"}) {
		t.Fatalf("child resources = %#v", got)
	}
	if _, ok, err := sources.Package("example.com/project/nested"); err != nil || !ok {
		t.Fatalf("nested package missing: ok=%t err=%v", ok, err)
	}
}

func resourcePaths(resources []ResourceFile) []string {
	paths := make([]string, len(resources))
	for index := range resources {
		paths[index] = resources[index].Path
	}
	return paths
}
