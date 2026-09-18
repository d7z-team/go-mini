package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestDirectorySourcesLoadPackages(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.mgo", "package main\n")
	write("lib/lib.mgo", "package lib\n")
	write("lib/lib_test.mgo", "package lib\n")
	module, err := workspace.LoadSources(t.Context(), root, "example.com/project", nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := module.Sources.PackagePaths()
	if len(paths) != 2 || paths[0] != "example.com/project" || paths[1] != "example.com/project/lib" {
		t.Fatalf("unexpected packages: %#v", paths)
	}
	pkg, ok, err := module.Sources.Package("example.com/project/lib")
	if err != nil || !ok || len(pkg.Files) != 1 || len(pkg.TestFiles) != 1 {
		t.Fatalf("unexpected lib package: ok=%v err=%v %#v", ok, err, pkg)
	}
}

func TestFilesystemAndMemorySourcesCompileIdentically(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.mgo":    "package main\nimport \"example.com/project/lib\"\nfunc Main() int { return lib.Value() }\n",
		"lib/lib.mgo": "package lib\nfunc Value() int { return 42 }\n",
	}
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	filesystem, err := workspace.LoadSources(t.Context(), root, "example.com/project", nil)
	if err != nil {
		t.Fatal(err)
	}
	memory, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ID:         workspace.PackageID{Namespace: "module:example.com/project"},
			ModulePath: "example.com/project",
			Files:      []source.File{{Path: "main.mgo", Text: files["main.mgo"]}},
			Resources: []workspace.ResourceFile{
				{Path: "main.mgo", Data: []byte(files["main.mgo"])},
				{Path: "lib/lib.mgo", Data: []byte(files["lib/lib.mgo"])},
			},
		},
		{
			ID:         workspace.PackageID{Namespace: "module:example.com/project", Path: "lib"},
			ModulePath: "example.com/project/lib",
			Files:      []source.File{{Path: "lib/lib.mgo", Text: files["lib/lib.mgo"]}},
			Resources:  []workspace.ResourceFile{{Path: "lib.mgo", Data: []byte(files["lib/lib.mgo"])}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(sources workspace.SourceSet) compiler.Result {
		t.Helper()
		result, err := compiler.Compile(compiler.Request{Root: "example.com/project", Sources: sources})
		if err != nil || !result.OK() {
			t.Fatalf("compile: err=%v diagnostics=%#v", err, result.Diagnostics)
		}
		return result
	}
	left := compile(filesystem.Sources)
	right := compile(memory)
	leftRoot := left.Artifacts["example.com/project"]
	rightRoot := right.Artifacts["example.com/project"]
	leftHash, err := ir.Hash(&leftRoot)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := ir.Hash(&rightRoot)
	if err != nil {
		t.Fatal(err)
	}
	if left.GraphHash != right.GraphHash || leftHash != rightHash {
		t.Fatalf("provider changed output: graph %s/%s artifact %s/%s", left.GraphHash, right.GraphHash, leftHash, rightHash)
	}
}
