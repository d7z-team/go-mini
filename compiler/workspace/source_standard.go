package workspace

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type standardSourceSet struct {
	sets     []SourceSet
	paths    []string
	owners   map[string][]int
	packages map[string]*standardPackage
}

type standardPackage struct {
	once   sync.Once
	source SourcePackage
	err    error
}

// ComposeStandardSourceSets combines additive standard-library source trees.
// Packages with the same import path are merged by file; ordinary source sets
// should continue to use MergeSourceSets, which enforces a single owner.
func ComposeStandardSourceSets(sets ...SourceSet) (SourceSet, error) {
	out := &standardSourceSet{
		sets:     append([]SourceSet(nil), sets...),
		owners:   make(map[string][]int),
		packages: make(map[string]*standardPackage),
	}
	for index, set := range out.sets {
		if set == nil {
			return nil, errors.New("nil standard source set")
		}
		paths, err := set.PackagePaths()
		if err != nil {
			return nil, err
		}
		for _, modulePath := range paths {
			modulePath, err = normalizeModulePath(modulePath)
			if err != nil {
				return nil, err
			}
			if _, exists := out.owners[modulePath]; !exists {
				out.paths = append(out.paths, modulePath)
				out.packages[modulePath] = &standardPackage{}
			}
			out.owners[modulePath] = append(out.owners[modulePath], index)
		}
	}
	sort.Strings(out.paths)
	return out, nil
}

func (s *standardSourceSet) PackagePaths() ([]string, error) {
	return append([]string(nil), s.paths...), nil
}

func (s *standardSourceSet) Package(modulePath string) (SourcePackage, bool, error) {
	modulePath = strings.TrimSpace(modulePath)
	entry, found := s.packages[modulePath]
	if !found {
		return SourcePackage{}, false, nil
	}
	entry.once.Do(func() {
		merged := SourcePackage{ID: PackageID{Namespace: "std", Path: modulePath}, ModulePath: modulePath}
		for _, owner := range s.owners[modulePath] {
			pkg, exists, err := s.sets[owner].Package(modulePath)
			if err != nil {
				entry.err = err
				return
			}
			if !exists {
				entry.err = fmt.Errorf("standard source set advertised package %q but did not provide it", modulePath)
				return
			}
			if pkg.ID.Namespace != "std" || pkg.ID.Path != modulePath {
				entry.err = fmt.Errorf("standard package %q has identity %q", modulePath, pkg.ID.String())
				return
			}
			if len(merged.SelectionTarget.Tags) == 0 {
				merged.SelectionTarget = pkg.SelectionTarget
			} else if !merged.SelectionTarget.Equal(pkg.SelectionTarget) {
				entry.err = fmt.Errorf("standard package %q has inconsistent source targets", modulePath)
				return
			}
			merged.Files = append(merged.Files, pkg.Files...)
			merged.TestFiles = append(merged.TestFiles, pkg.TestFiles...)
			merged.Resources = append(merged.Resources, pkg.Resources...)
			merged.SourceCandidates = append(merged.SourceCandidates, pkg.SourceCandidates...)
		}
		if entry.err == nil {
			entry.source, entry.err = normalizePackage(merged)
		}
	})
	if entry.err != nil {
		return SourcePackage{}, true, entry.err
	}
	return clonePackage(entry.source), true, nil
}
