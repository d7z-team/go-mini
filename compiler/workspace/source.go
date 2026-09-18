package workspace

import (
	"fmt"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

type SourceSet interface {
	PackagePaths() ([]string, error)
	Package(modulePath string) (SourcePackage, bool, error)
}

type SourcePackage struct {
	ID               PackageID
	ModulePath       string
	Files            []source.File
	TestFiles        []source.File
	Resources        []ResourceFile
	SelectionTarget  target.Target
	SourceCandidates []SourceCandidate
}

// ResourceFile is immutable package-relative data available to //go:embed.
type ResourceFile struct {
	Path string
	Data []byte
	Hash string
}

// SourceCandidate records target selection material for one source file.
type SourceCandidate struct {
	Path     string
	Hash     string
	Size     int
	Selected bool
	Test     bool
}

type MemorySourceSet struct {
	packages map[string]SourcePackage
	paths    []string
}

func NewMemorySourceSet(packages []SourcePackage) (MemorySourceSet, error) {
	out := MemorySourceSet{packages: make(map[string]SourcePackage, len(packages))}
	for _, source := range packages {
		pkg, err := normalizePackage(source)
		if err != nil {
			return MemorySourceSet{}, err
		}
		if _, exists := out.packages[pkg.ModulePath]; exists {
			return MemorySourceSet{}, fmt.Errorf("duplicate source package %q", pkg.ModulePath)
		}
		out.packages[pkg.ModulePath] = pkg
		out.paths = append(out.paths, pkg.ModulePath)
	}
	sort.Strings(out.paths)
	return out, nil
}

func (s MemorySourceSet) PackagePaths() ([]string, error) {
	return append([]string(nil), s.paths...), nil
}

func (s MemorySourceSet) Package(modulePath string) (SourcePackage, bool, error) {
	pkg, ok := s.packages[strings.TrimSpace(modulePath)]
	if !ok {
		return SourcePackage{}, false, nil
	}
	return clonePackage(pkg), true, nil
}
