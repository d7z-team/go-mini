package doc_test

import (
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/d7z-team/mini-go/compiler/analysis"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/stdlib"
	docgen "github.com/d7z-team/mini-go/tooling/doc"
)

func TestStandardLibraryBuildsOneDocumentationCatalog(t *testing.T) {
	sources, err := workspace.StandardLibrary(stdlib.Open())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := sources.PackagePaths()
	if err != nil {
		t.Fatal(err)
	}
	checked, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Roots: paths, Sources: sources})
	if err != nil || len(checked.Diagnostics) != 0 {
		t.Fatalf("check stdlib: diagnostics=%#v err=%v", checked.Diagnostics, err)
	}
	catalog, err := docgen.Build(docgen.BuildRequest{Workspace: checked, Packages: paths})
	if err != nil {
		t.Fatal(err)
	}
	output, err := docgen.Render(catalog)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var missingDocs []string
	for _, file := range output.Files {
		if seen[file.Path] {
			t.Fatalf("duplicate generated path %q", file.Path)
		}
		seen[file.Path] = true
		if len(file.Text) == 0 || file.Text[len(file.Text)-1] != '\n' {
			t.Fatalf("generated file %q has no final newline", file.Path)
		}
	}
	for _, pkg := range catalog.Packages {
		if pkg.Generate && !seen["packages/"+pkg.ModulePath+".md"] {
			t.Fatalf("package %q has no generated page", pkg.ModulePath)
		}
		documented := map[string]bool{}
		if pkg.Doc.Text == "" {
			missingDocs = append(missingDocs, "package "+pkg.ModulePath)
		}
		for _, symbol := range pkg.Symbols {
			documented[symbol.Name] = true
			if !symbol.Exported {
				continue
			}
			if symbol.Doc.Text == "" {
				missingDocs = append(missingDocs, symbol.Kind+" "+pkg.ModulePath+"."+symbol.Name)
			}
			for _, member := range symbol.Members {
				if member.Exported && !member.Embedded && member.Doc.Text == "" {
					missingDocs = append(missingDocs, member.Kind+" "+pkg.ModulePath+"."+symbol.Name+"."+member.Name)
				}
			}
			for _, method := range symbol.Methods {
				if method.Exported && method.Doc.Text == "" {
					missingDocs = append(missingDocs, "method "+pkg.ModulePath+"."+symbol.Name+"."+method.Name)
				}
			}
		}
		info := checked.Packages[pkg.ModulePath].Checked.Info
		for name, objectID := range info.Scope(info.PackageScope).Objects {
			object := info.Objects[objectID]
			r, _ := utf8.DecodeRuneInString(name)
			if !unicode.IsUpper(r) || object.Kind == check.ObjectImport {
				continue
			}
			if !documented[name] {
				t.Fatalf("exported semantic symbol %s.%s is absent from the documentation catalog", pkg.ModulePath, name)
			}
		}
	}
	if len(missingDocs) != 0 {
		sort.Strings(missingDocs)
		t.Fatalf("stdlib documentation is incomplete:\n%s", strings.Join(missingDocs, "\n"))
	}
}
