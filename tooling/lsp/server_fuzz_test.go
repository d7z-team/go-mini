package lsp

import (
	"context"
	"testing"

	protocol "go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func FuzzProtocolServerLifecycle(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{2, 0, 1, 2, 3})
	f.Add([]byte{0, 0, 2, 2, 3})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			return
		}
		set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
			ModulePath: "fuzz",
			Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		server := NewProtocolServer(func(context.Context, WorkspaceOptions) (Workspace, error) {
			return Workspace{RootPath: "/workspace", ModulePath: "fuzz", Sources: set}, nil
		})
		params := &protocol.InitializeParams{WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.File("/workspace"), Name: "workspace"}}),
		}}
		for _, operation := range operations {
			switch operation % 4 {
			case 0:
				_, _ = server.Initialize(context.Background(), params)
			case 1:
				_ = server.Initialized(context.Background(), nil)
			case 2:
				_ = server.Shutdown(context.Background())
			case 3:
				_ = server.Exit(context.Background())
			}
		}
	})
}
