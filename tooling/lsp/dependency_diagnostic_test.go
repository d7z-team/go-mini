package lsp

import (
	"context"
	"errors"
	"testing"

	protocol "go.lsp.dev/protocol"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestDependencyDiagnosticPreservesSourceAndProjectsImport(t *testing.T) {
	root := workspace.SourcePackage{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"dependency\"\nfunc Main() { _ = dependency.Value() }\n"}}}
	dependency := workspace.SourcePackage{ModulePath: "dependency", Files: []source.File{{Path: "main.mgo", Text: "package dependency\nfunc Value() int8 { return 128 }\n"}}}
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{root, dependency})
	if err != nil {
		t.Fatal(err)
	}
	documents, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{root})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(Config{Root: "example", Sources: sources, Documents: documents})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	rootURI, _, ok := snapshot.Document("example", "main.mgo")
	if !ok {
		t.Fatal("root document missing")
	}
	dependencyURI, _, ok := snapshot.Document("dependency", "main.mgo")
	if !ok {
		t.Fatal("dependency document missing")
	}
	projected := snapshot.Diagnostics(rootURI)
	if len(projected) == 0 || projected[0].Range.Start.Line != 1 || len(projected[0].RelatedInformation) != 1 || projected[0].RelatedInformation[0].Location.URI != dependencyURI {
		t.Fatalf("import diagnostics: %#v", projected)
	}
	wire := wireDiagnostics(projected)
	if len(wire[0].RelatedInformation) != 1 || string(wire[0].RelatedInformation[0].Location.URI) != string(dependencyURI) {
		t.Fatalf("wire lost origin: %#v", wire)
	}
	if len(snapshot.Diagnostics(dependencyURI)) == 0 {
		t.Fatal("source diagnostic lost")
	}
}

func TestImportedMemberDiagnosticUsesSelectorLocation(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport alias \"dependency\"\nfunc Main() { alias.Missing() }\n"}}},
		{ModulePath: "dependency", Files: []source.File{{Path: "lib.mgo", Text: "package dependency\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(Config{Root: "example", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	uri, _, ok := snapshot.Document("example", "main.mgo")
	if !ok {
		t.Fatal("root document missing")
	}
	diagnostics := snapshot.Diagnostics(uri)
	if len(diagnostics) != 1 || diagnostics[0].Range.Start.Line != 2 || diagnostics[0].Range.Start.Character != 14 {
		t.Fatalf("selector diagnostic: %#v", diagnostics)
	}
}

type unreadableSources struct{}

func (unreadableSources) PackagePaths() ([]string, error) {
	return nil, errors.New("controlled source discovery failure")
}

func (unreadableSources) Package(string) (workspace.SourcePackage, bool, error) {
	return workspace.SourcePackage{}, false, errors.New("controlled source read failure")
}

type workspaceDiagnosticClient struct {
	protocol.UnimplementedClient
	publications []*protocol.PublishDiagnosticsParams
	messages     []*protocol.ShowMessageParams
}

func (c *workspaceDiagnosticClient) PublishDiagnostics(_ context.Context, p *protocol.PublishDiagnosticsParams) error {
	c.publications = append(c.publications, p)
	return nil
}

func (c *workspaceDiagnosticClient) ShowMessage(_ context.Context, p *protocol.ShowMessageParams) error {
	c.messages = append(c.messages, p)
	return nil
}

func TestWorkspaceSourceFailureIsPublishedAndCleared(t *testing.T) {
	engine, err := NewEngine(Config{Root: "example", Sources: unreadableSources{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(engine.Snapshot().WorkspaceDiagnostics()) == 0 {
		t.Fatal("source discovery error lost")
	}
	root := t.TempDir()
	server := NewProtocolServer(nil)
	server.engine = engine
	server.workspace.RootPath = root
	client := &workspaceDiagnosticClient{}
	ctx := protocol.WithClient(context.Background(), client)
	if err := server.publishChanged(ctx); err != nil {
		t.Fatal(err)
	}
	if len(client.publications) == 0 || len(client.publications[0].Diagnostics) == 0 || len(client.messages) == 0 {
		t.Fatal("workspace diagnostic not visible")
	}
	previous := len(client.messages)
	if err := server.publishChanged(ctx); err != nil {
		t.Fatal(err)
	}
	if len(client.messages) != previous {
		t.Fatal("unchanged workspace message repeated")
	}
	server.engine, _ = testEngine(t, "package example\n")
	if err := server.publishChanged(ctx); err != nil {
		t.Fatal(err)
	}
	if len(client.publications[len(client.publications)-1].Diagnostics) != 0 {
		t.Fatal("workspace diagnostic not cleared")
	}
}
