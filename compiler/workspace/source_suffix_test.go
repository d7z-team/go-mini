package workspace_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestSourceDiscoverySeparatesScriptsAndHostResources(t *testing.T) {
	tree := fstest.MapFS{
		"app/main.mgo":        {Data: []byte("package main\n")},
		"app/main_test.mgo":   {Data: []byte("package main\n")},
		"app/binding.go":      {Data: []byte("host source, not MiniGo syntax")},
		"app/binding_test.go": {Data: []byte("host test")},
		"host/only.go":        {Data: []byte("host package")},
	}
	for _, mode := range []string{"eager", "indexed", "standard"} {
		t.Run(mode, func(t *testing.T) {
			var sources workspace.SourceSet
			var err error
			packagePath := "example/app"
			switch mode {
			case "eager":
				sources, err = workspace.DiscoverSourceTree(tree, ".", "example")
			case "indexed":
				sources, err = workspace.DiscoverIndexedSourceTree(tree, ".", "example")
			case "standard":
				sources, err = workspace.StandardLibrary(tree)
				packagePath = "app"
			}
			if err != nil {
				t.Fatal(err)
			}
			paths, err := sources.PackagePaths()
			if err != nil || !reflect.DeepEqual(paths, []string{packagePath}) {
				t.Fatalf("packages = %v, %v", paths, err)
			}
			pkg, ok, err := sources.Package(packagePath)
			if err != nil || !ok {
				t.Fatalf("package = %v, %v", ok, err)
			}
			if len(pkg.Files) != 1 || pkg.Files[0].Path != "app/main.mgo" || len(pkg.TestFiles) != 1 || pkg.TestFiles[0].Path != "app/main_test.mgo" {
				t.Fatalf("source classification: files=%v tests=%v", pkg.Files, pkg.TestFiles)
			}
			resources := map[string]string{}
			for _, file := range pkg.Resources {
				resources[file.Path] = string(file.Data)
			}
			if resources["binding.go"] != "host source, not MiniGo syntax" || resources["binding_test.go"] != "host test" {
				t.Fatalf("host resources = %v", resources)
			}
			if _, diagnostics, err := workspace.ParsePackage(pkg); err != nil || len(diagnostics) != 0 {
				t.Fatalf("parse = %v, %v", diagnostics, err)
			}
		})
	}
}

func TestSourceInputsValidateSuffix(t *testing.T) {
	base, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binding.go", "binding_test.go", "binding.txt"} {
		t.Run(name, func(t *testing.T) {
			_, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example", Files: []source.File{{Path: name, Text: "package main\n"}}}})
			if err == nil || !strings.Contains(err.Error(), "must use .mgo") {
				t.Fatalf("memory error = %v", err)
			}
			_, err = workspace.Overlay(base, []workspace.SourceChange{{ModulePath: "example", Path: name, Text: "package main\n"}})
			if err == nil || !strings.Contains(err.Error(), "must use .mgo") {
				t.Fatalf("overlay error = %v", err)
			}
		})
	}
}

func TestSourceOriginPreservesLocationsAndIdentity(t *testing.T) {
	var previousHash, previousCandidate string
	for _, origin := range []string{"first.go", "second.go"} {
		pkg := workspace.SourcePackage{ModulePath: "example", Files: []source.File{{Path: "input.mgo", OriginPath: origin, Text: "package main\nfunc invalid( {\n"}}}
		selected, diagnostics, err := workspace.SelectPackage(pkg, target.Target{})
		if err != nil || len(diagnostics) != 0 {
			t.Fatalf("select = %v, %v", diagnostics, err)
		}
		header, _, err := workspace.ScanPackageHeader(selected)
		if err != nil {
			t.Fatal(err)
		}
		parsed, diagnostics, err := workspace.ParsePackage(header.Source)
		if err != nil || len(diagnostics) == 0 {
			t.Fatalf("parse = %v, %v", diagnostics, err)
		}
		if parsed.Source.Files[0].Path != "input.mgo" || parsed.Program.Files[0].Path != origin || diagnostics[0].Primary.Start.File != origin {
			t.Fatalf("origin not preserved: source=%v ast=%v diagnostics=%v", parsed.Source.Files, parsed.Program.Files, diagnostics)
		}
		if header.Hash == previousHash || selected.SourceCandidates[0].Hash == previousCandidate {
			t.Fatal("origin change reused source identity")
		}
		previousHash, previousCandidate = header.Hash, selected.SourceCandidates[0].Hash
	}
}
