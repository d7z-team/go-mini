package workspace

import (
	"errors"
	"fmt"
	"sort"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

// SourceChange replaces, adds, or deletes one source file in a SourceSet.
type SourceChange struct {
	ModulePath string
	Path       string
	Text       string
	Deleted    bool
}

// Overlay returns an immutable source snapshot with changes applied to base.
func Overlay(base SourceSet, changes []SourceChange) (SourceSet, error) {
	if base == nil {
		return nil, errors.New("nil base source set")
	}
	packages := map[string]SourcePackage{}
	paths, err := base.PackagePaths()
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	for _, change := range changes {
		modulePath, err := normalizeModulePath(change.ModulePath)
		if err != nil {
			return nil, err
		}
		name, err := normalizeFilePath(change.Path)
		if err != nil {
			return nil, err
		}
		if !isSourceFile(name) {
			return nil, fmt.Errorf("source file %q must use .mgo", name)
		}
		key := modulePath + "\x00" + name
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate source change for %q in package %q", name, modulePath)
		}
		seen[key] = struct{}{}
		pkg, loaded := packages[modulePath]
		if !loaded {
			var err error
			pkg, _, err = base.Package(modulePath)
			if err != nil {
				return nil, err
			}
		}
		origin := ""
		remove := func(files []source.File) []source.File {
			out := files[:0]
			for _, file := range files {
				if file.Path != name {
					out = append(out, file)
				} else {
					origin = file.OriginPath
				}
			}
			return out
		}
		pkg.Files = remove(pkg.Files)
		pkg.TestFiles = remove(pkg.TestFiles)
		selected := true
		if len(pkg.SourceCandidates) != 0 {
			candidates := pkg.SourceCandidates[:0]
			for _, candidate := range pkg.SourceCandidates {
				if candidate.Path != name {
					candidates = append(candidates, candidate)
				}
			}
			if !change.Deleted {
				selected, err = target.MatchSource(change.Text, pkg.SelectionTarget)
				if err != nil {
					return nil, err
				}
				hash := source.HashText(change.Text)
				if origin != "" {
					hash = source.HashText(origin + "\x00" + hash)
				}
				candidates = append(candidates, SourceCandidate{Path: name, Hash: hash, Size: len(change.Text), Selected: selected, Test: isTestFile(name)})
			}
			pkg.SourceCandidates = candidates
		}
		if change.Deleted || !selected {
			packages[modulePath] = pkg
			continue
		}
		pkg.ModulePath = modulePath
		file := source.File{Path: name, Text: change.Text, OriginPath: origin}
		switch {
		case isTestFile(name):
			pkg.TestFiles = append(pkg.TestFiles, file)
		default:
			pkg.Files = append(pkg.Files, file)
		}
		packages[modulePath] = pkg
	}

	result := make([]SourcePackage, 0, len(packages))
	for _, pkg := range packages {
		if len(pkg.Files) != 0 {
			result = append(result, pkg)
		}
	}
	changed, err := NewMemorySourceSet(result)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]bool, len(paths)+len(packages))
	for _, path := range paths {
		visible[path] = true
	}
	for path, pkg := range packages {
		if len(pkg.Files) == 0 {
			delete(visible, path)
		} else {
			visible[path] = true
		}
	}
	paths = make([]string, 0, len(visible))
	for path := range visible {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return &overlaySourceSet{base: base, changed: changed, overridden: packages, paths: paths}, nil
}

type overlaySourceSet struct {
	base       SourceSet
	changed    MemorySourceSet
	overridden map[string]SourcePackage
	paths      []string
}

func (s *overlaySourceSet) PackagePaths() ([]string, error) {
	return append([]string(nil), s.paths...), nil
}

func (s *overlaySourceSet) Package(path string) (SourcePackage, bool, error) {
	if _, ok := s.overridden[path]; ok {
		return s.changed.Package(path)
	}
	return s.base.Package(path)
}
