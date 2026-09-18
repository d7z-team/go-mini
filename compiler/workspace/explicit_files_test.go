package workspace_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestNewExplicitSourceSetBuildsStablePackageAndSelectedResources(t *testing.T) {
	directory := t.TempDir()
	writeExplicitFile(t, directory, "assets/value.txt", "selected")
	writeExplicitFile(t, directory, "assets/ignored.txt", "ignored")

	loaded, err := workspace.NewExplicitSourceSet(os.DirFS(directory), []source.File{
		{Path: "helper.mgo", Text: "package main\nfunc helper() int { return 1 }\n"},
		{Path: "main.mgo", Text: `package main
import _ "embed"
//go:embed assets/value.txt
var value string
func main() {}
`},
	}, target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := loaded.PackagePaths()
	if err != nil || !reflect.DeepEqual(paths, []string{workspace.CommandLinePackage}) {
		t.Fatalf("PackagePaths = %#v, %v", paths, err)
	}
	pkg, exists, err := loaded.Package(workspace.CommandLinePackage)
	if err != nil || !exists {
		t.Fatalf("Package exists=%v error=%v", exists, err)
	}
	if pkg.ID.Namespace != "command" || pkg.ID.Path != "files" {
		t.Fatalf("Package ID = %#v", pkg.ID)
	}
	if got := []string{pkg.Files[0].Path, pkg.Files[1].Path}; !reflect.DeepEqual(got, []string{"helper.mgo", "main.mgo"}) {
		t.Fatalf("source paths = %#v", got)
	}
	if len(pkg.Resources) != 1 || pkg.Resources[0].Path != "assets/value.txt" || string(pkg.Resources[0].Data) != "selected" {
		t.Fatalf("resources = %#v", pkg.Resources)
	}
	if _, diagnostics, err := workspace.ParsePackage(pkg); err != nil || len(diagnostics) != 0 {
		t.Fatalf("ParsePackage error=%v diagnostics=%#v", err, diagnostics)
	}
}

func TestNewExplicitSourceSetRejectsInvalidLists(t *testing.T) {
	directory := t.TempDir()

	tests := []struct {
		name  string
		files []source.File
	}{
		{name: "empty"},
		{name: "duplicate", files: []source.File{{Path: "main.mgo"}, {Path: "main.mgo"}}},
		{name: "test", files: []source.File{{Path: "main_test.mgo"}}},
		{name: "suffix", files: []source.File{{Path: "text.txt"}}},
		{name: "host source", files: []source.File{{Path: "main.go"}}},
		{name: "path", files: []source.File{{Path: "sub/main.mgo"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := workspace.NewExplicitSourceSet(os.DirFS(directory), test.files, target.Target{}); err == nil {
				t.Fatal("NewExplicitSourceSet succeeded")
			}
		})
	}
}

func TestNewExplicitSourceSetKeepsResourcesFromValidEmbedPatterns(t *testing.T) {
	directory := t.TempDir()
	writeExplicitFile(t, directory, "main.mgo", `package main
import _ "embed"
//go:embed present.txt
var present string
//go:embed missing.txt
var missing string
func main() {}
`)
	writeExplicitFile(t, directory, "present.txt", "present")

	loaded, err := workspace.NewExplicitSourceSet(os.DirFS(directory), []source.File{{Path: "main.mgo", Text: `package main
import _ "embed"
//go:embed present.txt
var present string
//go:embed missing.txt
var missing string
func main() {}
`}}, target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	pkg, exists, err := loaded.Package(workspace.CommandLinePackage)
	if err != nil || !exists {
		t.Fatalf("Package exists=%v error=%v", exists, err)
	}
	if len(pkg.Resources) != 1 || pkg.Resources[0].Path != "present.txt" {
		t.Fatalf("resources = %#v", pkg.Resources)
	}
}

func writeExplicitFile(t *testing.T, root, name, text string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
