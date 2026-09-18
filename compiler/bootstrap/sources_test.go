package bootstrap

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCompilerSourceTreePreservesOriginsAndResources(t *testing.T) {
	text := "package entry\nimport _ \"embed\"\n//go:embed entry.go\nvar source string\n"
	tree := fstest.MapFS{
		"compiler/entry/entry.go":           {Data: []byte(text)},
		"compiler/entry/entry_test.go":      {Data: []byte("invalid host test")},
		"compiler/entry/data.bin":           {Data: []byte{0, 255}},
		"compiler/entry/_assets/secret.txt": {Data: []byte("hidden resource")},
	}
	sources, err := compilerSourceTree(tree, "compiler", compilerModulePath)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ok, err := sources.Package(compilerModulePath + "/entry")
	if err != nil || !ok {
		t.Fatalf("package = %v, %v", ok, err)
	}
	if len(pkg.Files) != 1 || pkg.Files[0].Path != "entry/entry.go.mgo" || pkg.Files[0].OriginPath != "entry/entry.go" || len(pkg.TestFiles) != 0 {
		t.Fatalf("mapped sources = %v, tests=%v", pkg.Files, pkg.TestFiles)
	}
	resources := map[string]string{}
	for _, file := range pkg.Resources {
		resources[file.Path] = string(file.Data)
	}
	if resources["entry.go"] != text || resources["data.bin"] != string([]byte{0, 255}) || resources["entry.go.mgo"] != "" {
		t.Fatalf("resource tree = %v", resources)
	}
	if resources["_assets/secret.txt"] != "hidden resource" {
		t.Fatalf("hidden resource tree = %v", resources)
	}
	parsed, diagnostics, err := workspace.ParsePackage(pkg)
	if err != nil || len(diagnostics) != 0 || parsed.Program.Files[0].Path != "entry/entry.go" {
		t.Fatalf("parse = %v, %v, %v", parsed.Program.Files, diagnostics, err)
	}
	tree["compiler/entry/entry.go.mgo"] = &fstest.MapFile{Data: []byte("package entry\n")}
	if _, err := compilerSourceTree(tree, "compiler", compilerModulePath); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("collision error = %v", err)
	}
}
