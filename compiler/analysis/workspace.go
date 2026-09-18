package analysis

import (
	"context"

	build "github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type WorkspaceRequest struct {
	Context  context.Context
	Root     string
	Roots    []string
	Target   target.Target
	Sources  workspace.SourceSet
	Limits   build.Limits
	Previous *WorkspaceResult
}

type WorkspaceResult struct {
	Target           target.Target
	Limits           build.Limits
	Packages         map[string]Package
	ExportHashes     map[string]string
	SourceHashes     map[string]string
	DependencyHashes map[string]string
	Diagnostics      []source.Diagnostic
	Stats            build.Stats
}

// CheckWorkspace projects compiler-owned semantic analysis into tooling indexes.
func CheckWorkspace(request WorkspaceRequest) (WorkspaceResult, error) {
	var previous *build.AnalysisResult
	if request.Previous != nil {
		old := request.Previous
		previous = &build.AnalysisResult{
			Target: old.Target, Limits: old.Limits, SourceHashes: old.SourceHashes, DependencyHashes: old.DependencyHashes,
			ExportHashes: old.ExportHashes, Packages: make(map[string]build.AnalyzedPackage, len(old.Packages)),
		}
		for path, pkg := range old.Packages {
			previous.Packages[path] = build.AnalyzedPackage{Checked: pkg.Checked, Documents: pkg.Documents, Diagnostics: pkg.Diagnostics}
		}
	}
	analyzed, err := build.Analyze(build.AnalysisRequest{
		Request: build.Request{Context: request.Context, Root: request.Root, Sources: request.Sources, Target: request.Target, Limits: request.Limits},
		Roots:   request.Roots, Previous: previous, ContinueAfterErrors: true,
	})
	if err != nil {
		return WorkspaceResult{}, err
	}
	result := WorkspaceResult{
		Target: analyzed.Target, Limits: analyzed.Limits, Packages: make(map[string]Package, len(analyzed.Packages)),
		ExportHashes: analyzed.ExportHashes, SourceHashes: analyzed.SourceHashes,
		DependencyHashes: analyzed.DependencyHashes, Diagnostics: analyzed.Diagnostics, Stats: analyzed.Stats,
	}
	for path, pkg := range analyzed.Packages {
		if request.Previous != nil {
			if old, ok := request.Previous.Packages[path]; ok && old.Checked.Info == pkg.Checked.Info {
				result.Packages[path] = old
				continue
			}
		}
		result.Packages[path] = Index(Package{Checked: pkg.Checked, Documents: pkg.Documents, Diagnostics: pkg.Diagnostics})
	}
	return result, nil
}
