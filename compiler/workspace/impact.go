package workspace

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

type ChangedFile struct {
	ModulePath string
	Path       string
}

// AffectedTestPackages returns packages whose tests may observe the supplied
// source edits. Production edits propagate through reverse imports; test-only
// edits remain local to their owning package.
func AffectedTestPackages(sources SourceSet, changed []ChangedFile, buildTarget target.Target) ([]string, error) {
	if sources == nil {
		return nil, errors.New("nil source set")
	}
	paths, err := sources.PackagePaths()
	if err != nil {
		return nil, err
	}
	hasTests := make(map[string]bool, len(paths))
	reverse := make(map[string][]string)
	for _, modulePath := range paths {
		pkg, ok, err := sources.Package(modulePath)
		if err != nil || !ok {
			return nil, fmt.Errorf("load package %q: ok=%v err=%v", modulePath, ok, err)
		}
		pkg, diagnostics, err := SelectPackage(pkg, buildTarget)
		if err != nil {
			return nil, err
		}
		if source.HasErrors(diagnostics) {
			return nil, fmt.Errorf("select package %q: %s", modulePath, diagnostics[0].Message)
		}
		header, diagnostics, err := ScanPackageHeader(pkg)
		if err != nil {
			return nil, err
		}
		if len(diagnostics) != 0 {
			return nil, fmt.Errorf("scan package %q: %s", modulePath, diagnostics[0].Message)
		}
		hasTests[modulePath] = len(pkg.TestFiles) != 0
		for _, dependency := range header.Imports {
			reverse[dependency] = append(reverse[dependency], modulePath)
		}
	}
	selected := make(map[string]struct{})
	queue := make([]string, 0, len(changed))
	queued := make(map[string]struct{})
	for _, file := range changed {
		modulePath := strings.TrimSpace(file.ModulePath)
		if modulePath == "" {
			return nil, fmt.Errorf("changed file %q has no module path", file.Path)
		}
		if hasTests[modulePath] {
			selected[modulePath] = struct{}{}
		}
		name := strings.TrimSpace(file.Path)
		if strings.HasSuffix(name, "_test.mgo") {
			continue
		}
		if _, exists := queued[modulePath]; !exists {
			queued[modulePath] = struct{}{}
			queue = append(queue, modulePath)
		}
	}
	for len(queue) != 0 {
		modulePath := queue[0]
		queue = queue[1:]
		for _, importer := range reverse[modulePath] {
			if hasTests[importer] {
				selected[importer] = struct{}{}
			}
			if _, exists := queued[importer]; !exists {
				queued[importer] = struct{}{}
				queue = append(queue, importer)
			}
		}
	}
	out := make([]string, 0, len(selected))
	for modulePath := range selected {
		out = append(out, modulePath)
	}
	sort.Strings(out)
	return out, nil
}
