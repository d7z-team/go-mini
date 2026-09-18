package compiler

import (
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

// resolvedSourceGraph owns the package headers used by one compile or Prepare
// attempt, including dependencies discovered after lowering.
type resolvedSourceGraph struct {
	target   target.Target
	packages map[string]workspace.PackageHeader
}

func newResolvedSourceGraph(graph workspace.HeaderGraph) *resolvedSourceGraph {
	return &resolvedSourceGraph{target: graph.Target, packages: graph.Packages}
}

func (g *resolvedSourceGraph) loadPackageClosure(request Request, modulePath string) (int, []source.Diagnostic, error) {
	if _, exists := g.packages[modulePath]; exists {
		return 0, nil, nil
	}
	dependencyGraph, err := loadHeaderGraph(request, []string{modulePath})
	if err != nil {
		return 0, nil, err
	}
	if len(dependencyGraph.Diagnostics) != 0 {
		return 0, dependencyGraph.Diagnostics, nil
	}
	added := 0
	for path, header := range dependencyGraph.Packages {
		if _, exists := g.packages[path]; exists {
			continue
		}
		g.packages[path] = header
		added++
	}
	return added, nil, nil
}

func (g *resolvedSourceGraph) hash(root string, order []string) string {
	return workspace.HashHeaderGraph([]string{root}, g.target, g.packages, order)
}
