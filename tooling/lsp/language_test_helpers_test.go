package lsp

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func testEngine(t *testing.T, text string) (*Engine, DocumentURI) {
	t.Helper()
	set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: text}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(Config{Root: "example", Sources: set, URI: func(_, _ string) DocumentURI { return "file:///main.mgo" }})
	if err != nil {
		t.Fatal(err)
	}
	return engine, "file:///main.mgo"
}
