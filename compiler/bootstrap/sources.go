package bootstrap

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/d7z-team/mini-go/compiler/workspace"
)

// compilerSourceTree adapts the compiler's Go sources without changing the
// source discovery rules or resource names seen by ordinary Mini-Go projects.
func compilerSourceTree(project fs.FS, root, modulePath string) (workspace.SourceSet, error) {
	tree, err := fs.Sub(project, root)
	if err != nil {
		return nil, err
	}
	var files []workspace.TreeFile
	origins := make(map[string]string)
	paths := make(map[string]bool)
	err = fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".hg", ".svn", ".bzr":
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := fs.ReadFile(tree, name)
		if err != nil {
			return err
		}
		paths[name] = true
		files = append(files, workspace.TreeFile{Path: name, Data: data})
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			logical := name + ".mgo"
			origins[logical] = name
			files = append(files, workspace.TreeFile{Path: logical, Data: data})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if original, mapped := origins[file.Path]; mapped && paths[file.Path] {
			logical := file.Path
			return nil, fmt.Errorf("bootstrap source %q collides with logical source for %q", logical, original)
		}
	}
	sources, err := workspace.NewTreeSourceSet(modulePath, files)
	if err != nil {
		return nil, err
	}
	packagePaths, err := sources.PackagePaths()
	if err != nil {
		return nil, err
	}
	packages := make([]workspace.SourcePackage, 0, len(packagePaths))
	for _, packagePath := range packagePaths {
		pkg, _, err := sources.Package(packagePath)
		if err != nil {
			return nil, err
		}
		for index := range pkg.Files {
			pkg.Files[index].OriginPath = origins[pkg.Files[index].Path]
		}
		// Only real files are visible to embed, not the logical source aliases.
		resources := pkg.Resources[:0]
		for _, resource := range pkg.Resources {
			if origins[path.Join(pkg.ID.Path, resource.Path)] == "" {
				resources = append(resources, resource)
			}
		}
		clear(pkg.Resources[len(resources):])
		pkg.Resources = resources
		packages = append(packages, pkg)
	}
	return workspace.NewMemorySourceSet(packages)
}
