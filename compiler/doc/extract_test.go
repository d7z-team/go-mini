package doc_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/analysis"
	"github.com/d7z-team/mini-go/compiler/doc"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestExtractMethodDocumentationFromSourceFacts(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "sample", Files: []source.File{{Path: "sample.mgo", Text: `// Package sample describes a counter.
package sample
// Counter stores a value.
type Counter struct { Value int }
// Read returns the current value.
func (c *Counter) Read() int {return c.Value}
`}}}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := analysis.CheckWorkspace(analysis.WorkspaceRequest{Context: t.Context(), Root: "sample", Sources: sources})
	if err != nil || source.HasErrors(facts.Diagnostics) {
		t.Fatalf("facts: %v, %v", err, facts.Diagnostics)
	}
	catalog, err := doc.Build(doc.BuildRequest{Workspace: facts, Packages: []string{"sample"}})
	if err != nil {
		t.Fatal(err)
	}
	comment, ok := catalog.LookupComment("sample", "*sample.Counter", "Read")
	if !ok || comment.Text != "Read returns the current value." || comment.Span.Start.File != "sample.mgo" {
		t.Fatalf("method comment: %+v, %v", comment, ok)
	}
}
