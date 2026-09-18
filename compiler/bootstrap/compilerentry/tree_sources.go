package compilerentry

import (
	"context"
	"fmt"
	"strings"

	"github.com/d7z-team/mini-go/compiler/workspace"
)

type SourceTree struct {
	ModulePath string
	Editable   *bool
	Files      []TreeFile
}

type TreeFile struct {
	Path string
	Data []byte
	URI  string
}

type SourcePackages struct{ Packages []Package }

func sourceTreePackages(ctx context.Context, trees []SourceTree) ([]Package, error) {
	packages := make([]Package, 0)
	var sets []workspace.SourceSet
	prefixes := map[string]bool{}
	for _, tree := range trees {
		tree.ModulePath = strings.TrimSpace(tree.ModulePath)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if prefixes[tree.ModulePath] {
			return nil, fmt.Errorf("duplicate source module %q", tree.ModulePath)
		}
		prefixes[tree.ModulePath] = true
		files := make([]workspace.TreeFile, len(tree.Files))
		uris := map[string]string{}
		for i, file := range tree.Files {
			files[i] = workspace.TreeFile{Path: file.Path, Data: file.Data}
			uris[file.Path] = file.URI
		}
		set, err := workspace.NewTreeSourceSet(tree.ModulePath, files)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
		paths, err := set.PackagePaths()
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			source, found, err := set.Package(path)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			pkg := Package{Files: []File{}, TestFiles: []File{}, Resources: []Resource{}, Editable: tree.Editable, Namespace: source.ID.Namespace, PackagePath: source.ID.Path, ModulePath: path}
			for _, file := range source.Files {
				pkg.Files = append(pkg.Files, File{Path: file.Path, Text: file.Text, URI: uris[file.Path]})
			}
			for _, file := range source.TestFiles {
				pkg.TestFiles = append(pkg.TestFiles, File{Path: file.Path, Text: file.Text, URI: uris[file.Path]})
			}
			for _, resource := range source.Resources {
				// Empty file bytes have one wire representation across native and VM execution.
				if len(resource.Data) == 0 {
					resource.Data = []byte{}
				}
				pkg.Resources = append(pkg.Resources, Resource{Path: resource.Path, Data: resource.Data})
			}
			packages = append(packages, pkg)
		}
	}
	if _, err := workspace.MergeSourceSets(sets...); err != nil {
		return nil, err
	}
	return packages, nil
}
