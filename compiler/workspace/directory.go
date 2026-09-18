//go:build !minigo

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DirectorySource maps a logical import prefix to a host directory.
type DirectorySource struct {
	Module    string `json:"module"`
	Directory string `json:"directory"`
}

// SourceLocation identifies one source file independently of its host location.
type SourceLocation struct {
	ModulePath string
	Path       string
}

// SourceLocations maps only registered files, never arbitrary paths.
type SourceLocations struct {
	files      map[SourceLocation]string
	identities map[string]SourceLocation
}

func (l *SourceLocations) File(modulePath, path string) (string, bool) {
	if l == nil {
		return "", false
	}
	file, ok := l.files[SourceLocation{ModulePath: modulePath, Path: path}]
	return file, ok
}

func (l *SourceLocations) Identity(file string) (SourceLocation, bool) {
	if l == nil {
		return SourceLocation{}, false
	}
	identity, ok := l.identities[filepath.Clean(file)]
	if !ok {
		if physical, err := filepath.EvalSymlinks(file); err == nil {
			identity, ok = l.identities[physical]
		}
	}
	return identity, ok
}

// Add indexes a stable source set below a physical root. Build the index before sharing it.
func (l *SourceLocations) Add(root string, sources SourceSet) error {
	if sources == nil {
		return errors.New("nil sources for location index")
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	paths, err := sources.PackagePaths()
	if err != nil {
		return err
	}
	files := make(map[SourceLocation]string)
	identities := make(map[string]SourceLocation)
	for _, packagePath := range paths {
		pkg, found, err := sources.Package(packagePath)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		for _, file := range append(pkg.Files, pkg.TestFiles...) {
			if !fs.ValidPath(file.Path) || strings.Contains(file.Path, "\\") {
				return fmt.Errorf("invalid source location %q", file.Path)
			}
			location := SourceLocation{ModulePath: packagePath, Path: file.Path}
			filename := filepath.Join(root, filepath.FromSlash(file.Path))
			if previous, ok := l.files[location]; ok && previous != filename {
				return fmt.Errorf("source identity %+v has multiple locations", location)
			}
			if previous, ok := l.identities[filename]; ok && previous != location {
				return fmt.Errorf("source file %q has multiple identities", filename)
			}
			if previous, ok := identities[filename]; ok && previous != location {
				return fmt.Errorf("source file %q has multiple identities", filename)
			}
			files[location] = filename
			identities[filename] = location
		}
	}
	if l.files == nil {
		l.files = map[SourceLocation]string{}
		l.identities = map[string]SourceLocation{}
	}
	for key, value := range files {
		l.files[key] = value
	}
	for key, value := range identities {
		l.identities[key] = value
	}
	return nil
}

type Directory struct {
	Root       string
	ModulePath string
	Sources    SourceSet
	Documents  SourceSet
	Locations  *SourceLocations
}

// LoadSources snapshots an application and explicit additional source roots.
// Additional directories are relative to root. Physical roots cannot overlap.
func LoadSources(ctx context.Context, root, modulePath string, additional []DirectorySource) (Directory, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Directory{}, err
	}
	if modulePath == "" {
		modulePath = "app"
	}
	mappings := append([]DirectorySource{{Module: modulePath, Directory: root}}, additional...)
	locations := &SourceLocations{files: map[SourceLocation]string{}, identities: map[string]SourceLocation{}}
	var sets []SourceSet
	var roots []string
	prefixes := map[string]bool{}
	for _, mapping := range mappings {
		if err := ctx.Err(); err != nil {
			return Directory{}, err
		}
		prefix, err := normalizeModulePath(mapping.Module)
		if err != nil {
			return Directory{}, err
		}
		if prefixes[prefix] {
			return Directory{}, fmt.Errorf("duplicate source module %q", prefix)
		}
		prefixes[prefix] = true
		directory := mapping.Directory
		if directory == "" {
			return Directory{}, fmt.Errorf("source %q has no directory", prefix)
		}
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(root, directory)
		}
		directory, err = filepath.EvalSymlinks(directory)
		if err != nil {
			return Directory{}, err
		}
		directory, err = filepath.Abs(directory)
		if err != nil {
			return Directory{}, err
		}
		for _, previous := range roots {
			relative, err := filepath.Rel(previous, directory)
			if err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				return Directory{}, fmt.Errorf("source directories overlap: %q and %q", previous, directory)
			}
			relative, err = filepath.Rel(directory, previous)
			if err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				return Directory{}, fmt.Errorf("source directories overlap: %q and %q", previous, directory)
			}
		}
		roots = append(roots, directory)
		set, err := DiscoverSourceTree(os.DirFS(directory), ".", prefix)
		if err != nil {
			return Directory{}, err
		}
		if err := locations.Add(directory, set); err != nil {
			return Directory{}, err
		}
		sets = append(sets, set)
	}
	if err := ctx.Err(); err != nil {
		return Directory{}, err
	}
	merged, err := MergeSourceSets(sets...)
	if err != nil {
		return Directory{}, err
	}
	return Directory{Root: roots[0], ModulePath: strings.TrimSpace(mappings[0].Module), Sources: merged, Documents: sets[0], Locations: locations}, nil
}
