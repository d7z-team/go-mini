package compiler

import (
	"context"
	"fmt"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type testSourceSnapshot struct {
	target      target.Target
	packages    []workspace.SourcePackage
	byPath      map[string]workspace.SourcePackage
	diagnostics map[string][]source.Diagnostic
	limits      []source.Diagnostic
}

func loadTestSourceSnapshot(ctx context.Context, sources workspace.SourceSet, buildTarget target.Target, limits Limits) (*testSourceSnapshot, error) {
	ctx = requestContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limits = normalizeCompilerLimits(limits)
	paths, err := sources.PackagePaths()
	if err != nil {
		return nil, err
	}
	snapshot := &testSourceSnapshot{
		target: buildTarget, packages: make([]workspace.SourcePackage, 0, len(paths)),
		byPath: make(map[string]workspace.SourcePackage, len(paths)), diagnostics: make(map[string][]source.Diagnostic),
	}
	if len(paths) > limits.MaxPackages {
		snapshot.limits = []source.Diagnostic{{Code: "compiler.limit.packages", Severity: source.SeverityError, Message: "source graph exceeds package limit"}}
		return snapshot, nil
	}
	fileCount, sourceBytes := 0, 0
	for _, modulePath := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pkg, exists, err := sources.Package(modulePath)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("source package %q disappeared while preparing tests", modulePath)
		}
		for _, files := range [][]source.File{pkg.Files, pkg.TestFiles} {
			for _, file := range files {
				fileCount++
				sourceBytes += len(file.Text)
				if len(file.Text) > limits.MaxSourceBytes {
					diagnostic := source.Diagnostic{Code: "scanner.source.limit", Severity: source.SeverityError, Message: "source file exceeds scanner byte limit"}
					diagnostic.Primary, _ = file.Span(0, len(file.Text))
					snapshot.limits = []source.Diagnostic{diagnostic}
					return snapshot, nil
				}
			}
		}
		if fileCount > limits.MaxFiles {
			snapshot.limits = []source.Diagnostic{{Code: "compiler.limit.files", Severity: source.SeverityError, Message: "source graph exceeds file limit"}}
			return snapshot, nil
		}
		if sourceBytes > limits.MaxTotalSourceBytes {
			snapshot.limits = []source.Diagnostic{{Code: "compiler.limit.source_bytes", Severity: source.SeverityError, Message: "source graph exceeds total byte limit"}}
			return snapshot, nil
		}
		pkg, diagnostics, err := workspace.SelectPackage(pkg, buildTarget)
		if err != nil {
			return nil, err
		}
		snapshot.packages = append(snapshot.packages, pkg)
		snapshot.byPath[modulePath] = pkg
		if source.HasErrors(diagnostics) {
			snapshot.diagnostics[modulePath] = diagnostics
		}
	}
	return snapshot, nil
}
