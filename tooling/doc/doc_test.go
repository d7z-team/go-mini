package doc_test

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/analysis"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
	docgen "github.com/d7z-team/mini-go/tooling/doc"
)

func TestBuildAndRenderMarkdownFromSelectedSourceFacts(t *testing.T) {
	const sourceText = `//go:build docs

// Package sample demonstrates generated documentation.
package sample

// Values contains the public constants.
const (
	// Answer is linked from [Value].
	Answer = 42
	Other  = 43
	hidden = 1
)

// Pair stores a value.
//
// Deprecated: use Value instead.
type Pair[T any] struct {
	// Left is the public value.
	Left T ` + "`json:\"left\"`" + `
	Right T // Right is trailing documentation.
	hidden int
}

// Reader reads values.
type Reader interface {
	/* Read consumes bytes. */
	Read([]byte) (int, error)
}

//extern is ordinary documentation.
//export is ordinary documentation too.
//line generated.mgo:1
//go:generate ignored
// Value returns [Answer]. Raw <script> is text and [unsafe] uses a blocked URL.
//
// [unsafe]: javascript:alert(1)
func Value() int { return Answer }

// Éclair demonstrates Unicode exported names.
func Éclair() {}

// Get returns the pair value.
func (p *Pair[T]) Get() T { return p.Left }

// BUG(parser): notes remain visible.
// across continued lines.
`
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/sample",
		Files: []source.File{
			{Path: "sample.mgo", Text: sourceText},
			{Path: "excluded.mgo", Text: "//go:build !docs\n\npackage sample\nfunc Broken( {\n"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	buildTarget := target.Target{Tags: []string{"docs"}}
	checked, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Roots: []string{"example/sample"}, Sources: sources, Target: buildTarget})
	if err != nil || len(checked.Diagnostics) != 0 {
		t.Fatalf("check source: diagnostics=%#v err=%v", checked.Diagnostics, err)
	}
	catalog, err := docgen.Build(docgen.BuildRequest{Workspace: checked, Packages: []string{"example/sample"}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := docgen.Render(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var markdown string
	for _, file := range output.Files {
		if file.Path == "packages/example/sample.md" {
			markdown = string(file.Text)
		}
	}
	for _, text := range []string{
		"# Package `example/sample`", "Package sample demonstrates", "const Answer", "Answer is linked",
		"const Other", "Values contains the public constants.",
		"type Pair[T any] struct { ... }", "Left T `json:\"left\"`", "Deprecated: use Value instead.",
		"Right is trailing documentation.", "Read consumes bytes.", "method Reader.Read", "Read([]byte) (int, error)",
		"func Value() int", "func (p *Pair[T]) Get() T", "BUG(parser)", "sample.md#const-answer",
		"notes remain visible. across continued lines.", "func Éclair()", "extern is ordinary documentation.",
		"export is ordinary documentation too.",
	} {
		if !strings.Contains(markdown, text) {
			t.Fatalf("generated Markdown does not contain %q:\n%s", text, markdown)
		}
	}
	for _, text := range []string{"go:build", "go:generate", "generated.mgo", "hidden int", "func Broken"} {
		if strings.Contains(markdown, text) {
			t.Fatalf("generated Markdown contains %q:\n%s", text, markdown)
		}
	}
	if strings.Contains(markdown, "](javascript:") || !strings.Contains(markdown, `\<script>`) {
		t.Fatalf("unsafe Markdown was not normalized:\n%s", markdown)
	}
	pairComment, ok := catalog.LookupComment("example/sample", "", "Pair")
	if !ok || !strings.Contains(pairComment.Text, "Pair stores") {
		t.Fatalf("pair comment: %#v, found=%v", pairComment, ok)
	}
	var pair docgen.Symbol
	for _, pkg := range catalog.Packages {
		for _, symbol := range pkg.Symbols {
			if symbol.Name == "Pair" {
				pair = symbol
			}
		}
	}
	if len(pair.TypeParameters) != 1 || pair.TypeParameters[0].Name != "T" || pair.TypeParameters[0].Constraint != "any" {
		t.Fatalf("pair type parameters: %#v", pair.TypeParameters)
	}
	if len(pair.Members) < 1 || pair.Members[0].RawTag != "`json:\"left\"`" || pair.Members[0].Tag != `json:"left"` {
		t.Fatalf("pair field tag: %#v", pair.Members)
	}
}

func TestRenderRejectsDuplicateAnchors(t *testing.T) {
	_, err := docgen.Render(docgen.Catalog{Packages: []docgen.Package{{
		ModulePath: "example/duplicate", Name: "duplicate", Generate: true,
		Symbols: []docgen.Symbol{
			{Kind: "func", Name: "Value", Exported: true, DisplaySignature: "func Value()"},
			{Kind: "func", Name: "Value", Exported: true, DisplaySignature: "func Value(int)"},
		},
	}}})
	if err == nil || !strings.Contains(err.Error(), "duplicate Markdown anchor") {
		t.Fatalf("duplicate anchor error: %v", err)
	}
}

func TestPackageDocumentationMergesAdjacentCommentForms(t *testing.T) {
	const sourceText = `/* Package sample keeps its synopsis. */
// The middle example remains part of the package documentation.
/* The final detail is retained too. */
package sample
`
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/sample", Files: []source.File{{Path: "doc.mgo", Text: sourceText}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Roots: []string{"example/sample"}, Sources: sources})
	if err != nil || len(checked.Diagnostics) != 0 {
		t.Fatalf("check source: diagnostics=%#v err=%v", checked.Diagnostics, err)
	}
	catalog, err := docgen.Build(docgen.BuildRequest{Workspace: checked, Packages: []string{"example/sample"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := catalog.Packages[0].Doc.Text
	for _, part := range []string{"Package sample keeps its synopsis.", "middle example", "final detail"} {
		if !strings.Contains(doc, part) {
			t.Fatalf("package documentation does not contain %q: %q", part, doc)
		}
	}
}

func TestRenderRejectsCaseFoldedPathCollisions(t *testing.T) {
	_, err := docgen.Render(docgen.Catalog{Packages: []docgen.Package{
		{ModulePath: "example/Value", Name: "upper", Generate: true},
		{ModulePath: "example/value", Name: "lower", Generate: true},
	}})
	if err == nil || !strings.Contains(err.Error(), "path collision") {
		t.Fatalf("path collision error: %v", err)
	}
}

func TestRenderIsDeterministicAcrossPackageSelectionOrder(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/a", Files: []source.File{{Path: "a.mgo", Text: "// Package a is A.\npackage a\nfunc A() {}\n"}}},
		{ModulePath: "example/b", Files: []source.File{{Path: "b.mgo", Text: "// Package b is B.\npackage b\nfunc B() {}\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Roots: []string{"example/a", "example/b"}, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	first, err := docgen.Build(docgen.BuildRequest{Workspace: checked, Packages: []string{"example/b", "example/a"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := docgen.Build(docgen.BuildRequest{Workspace: checked, Packages: []string{"example/a", "example/b"}})
	if err != nil {
		t.Fatal(err)
	}
	left, _ := docgen.Render(first)
	right, _ := docgen.Render(second)
	if len(left.Files) != len(right.Files) {
		t.Fatalf("file count differs: %d != %d", len(left.Files), len(right.Files))
	}
	for i := range left.Files {
		if left.Files[i].Path != right.Files[i].Path || string(left.Files[i].Text) != string(right.Files[i].Text) {
			t.Fatalf("output differs at %d", i)
		}
	}
}
