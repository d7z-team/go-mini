package main

import (
	"context"
	"io"

	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/stdlib"
	"github.com/d7z-team/mini-go/tooling/lsp"
)

func runLSPContext(ctx context.Context, environment commandEnvironment, input io.Reader, output io.Writer) error {
	return lsp.ServeProtocol(ctx, input, output, func(requestContext context.Context, options lsp.WorkspaceOptions) (lsp.Workspace, error) {
		root := options.Root
		if root == "" {
			root = environment.workingDir
		} else {
			root = environment.path(root)
		}
		module, err := workspace.LoadSources(requestContext, root, options.Module, options.Sources)
		if err != nil {
			return lsp.Workspace{}, err
		}
		standard, err := workspace.StandardLibrary(stdlib.Open())
		if err != nil {
			return lsp.Workspace{}, err
		}
		sources, err := workspace.MergeSourceSets(module.Sources, standard)
		if err != nil {
			return lsp.Workspace{}, err
		}
		return lsp.Workspace{RootPath: module.Root, ModulePath: module.ModulePath, Sources: sources, Documents: module.Documents, Locations: module.Locations}, nil
	})
}
