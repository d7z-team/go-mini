package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/d7z-team/mini-go/compiler/source"
)

// IndexedSourceSet indexes package ownership without reading file contents.
// Its filesystem must remain immutable for the lifetime of the source set.
// Package source and resources are loaded once when the import graph reaches
// that package and are released with the source set.
type IndexedSourceSet struct {
	tree        fs.FS
	modulePath  string
	paths       []string
	directories map[string]string
	entries     []string

	packages map[string]*indexedPackage
}

type indexedPackage struct {
	once   sync.Once
	source SourcePackage
	err    error
}

// DiscoverIndexedPackageTree maps directories to bare import paths while
// deferring all file reads until Package is called.
func DiscoverIndexedPackageTree(fsys fs.FS, root string) (SourceSet, error) {
	return discoverIndexedTree(fsys, root, "")
}

// DiscoverIndexedSourceTree maps a tree below one module path while deferring
// file reads until Package is called.
func DiscoverIndexedSourceTree(fsys fs.FS, root, modulePath string) (SourceSet, error) {
	modulePath, err := normalizeModulePath(modulePath)
	if err != nil {
		return nil, err
	}
	return discoverIndexedTree(fsys, root, modulePath)
}

func discoverIndexedTree(fsys fs.FS, root, modulePath string) (SourceSet, error) {
	if fsys == nil {
		return nil, errors.New("nil indexed source filesystem")
	}
	root, err := normalizeFSRoot(root)
	if err != nil {
		return nil, err
	}
	tree, err := fs.Sub(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("open source root %q: %w", root, err)
	}
	set := &IndexedSourceSet{
		tree: tree, modulePath: modulePath, directories: make(map[string]string),
		packages: make(map[string]*indexedPackage),
	}
	err = walkSourceFiles(tree, func(name string) error {
		classification := classifyPackageFile(name)
		if !classification.resource {
			return nil
		}
		set.entries = append(set.entries, name)
		if !classification.discoverPackage {
			return nil
		}
		directory := path.Dir(name)
		packagePath := modulePath
		if packagePath == "" {
			if directory == "." {
				return nil
			}
			packagePath = directory
		} else if directory != "." {
			packagePath += "/" + directory
		}
		set.directories[packagePath] = directory
		return nil
	})
	if err != nil {
		return nil, err
	}
	for packagePath := range set.directories {
		set.paths = append(set.paths, packagePath)
		set.packages[packagePath] = &indexedPackage{}
	}
	sort.Strings(set.paths)
	sort.Strings(set.entries)
	return set, nil
}

func (s *IndexedSourceSet) PackagePaths() ([]string, error) {
	if s == nil {
		return nil, errors.New("nil indexed source set")
	}
	return append([]string(nil), s.paths...), nil
}

func (s *IndexedSourceSet) Package(modulePath string) (SourcePackage, bool, error) {
	if s == nil {
		return SourcePackage{}, false, errors.New("nil indexed source set")
	}
	modulePath = strings.TrimSpace(modulePath)
	directory, exists := s.directories[modulePath]
	if !exists {
		return SourcePackage{}, false, nil
	}
	cached := s.packages[modulePath]
	cached.once.Do(func() { cached.source, cached.err = s.loadPackage(modulePath, directory) })
	return clonePackage(cached.source), true, cached.err
}

func (s *IndexedSourceSet) loadPackage(modulePath, directory string) (SourcePackage, error) {
	pkg := SourcePackage{ModulePath: modulePath, ID: PackageID{Namespace: "std", Path: modulePath}}
	if s.modulePath != "" {
		relative := strings.TrimPrefix(strings.TrimPrefix(modulePath, s.modulePath), "/")
		pkg.ID = PackageID{Namespace: "module:" + s.modulePath, Path: relative}
	}
	for _, name := range s.entries {
		classification := classifyPackageFile(name)
		directSource := classification.source && path.Dir(name) == directory
		resourcePath := name
		resource := directory == "."
		if directory != "." && strings.HasPrefix(name, directory+"/") {
			resource = true
			resourcePath = strings.TrimPrefix(name, directory+"/")
		}
		if !directSource && !resource {
			continue
		}
		data, err := fs.ReadFile(s.tree, name)
		if err != nil {
			return SourcePackage{}, fmt.Errorf("read package file %q: %w", name, err)
		}
		if directSource {
			file := source.File{Path: name, Text: string(data)}
			if classification.test {
				pkg.TestFiles = append(pkg.TestFiles, file)
			} else {
				pkg.Files = append(pkg.Files, file)
			}
		}
		if resource && classification.resource {
			pkg.Resources = append(pkg.Resources, ResourceFile{Path: resourcePath, Data: data, Hash: source.HashText(string(data))})
		}
	}
	return normalizePackage(pkg)
}
