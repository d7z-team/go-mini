package compilerentry

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type sourceSet struct {
	inputs      map[string]Package
	loaded      map[string]workspace.MemorySourceSet
	sourceFiles map[string][]string
	paths       []string
}

func newSourceSet(inputs []Package) (*sourceSet, error) {
	sources := &sourceSet{
		inputs:      make(map[string]Package, len(inputs)),
		loaded:      make(map[string]workspace.MemorySourceSet),
		sourceFiles: make(map[string][]string),
	}
	for _, input := range inputs {
		modulePath := strings.TrimSpace(input.ModulePath)
		if modulePath == "" {
			return nil, errors.New("source package has empty module path")
		}
		namespace := strings.TrimSpace(input.Namespace)
		if namespace == "" {
			return nil, fmt.Errorf("source package %q has empty namespace", modulePath)
		}
		if _, exists := sources.inputs[modulePath]; exists {
			return nil, fmt.Errorf("duplicate source package %q", modulePath)
		}
		input.Namespace = namespace
		input.PackagePath = strings.TrimSpace(input.PackagePath)
		input.ModulePath = modulePath
		sources.inputs[modulePath] = input
		sources.paths = append(sources.paths, modulePath)
		for _, files := range [][]File{input.Files, input.TestFiles} {
			for _, file := range files {
				if !strings.HasSuffix(file.Path, ".mgo") {
					return nil, fmt.Errorf("source file %q must use .mgo", file.Path)
				}
				sources.sourceFiles[file.Path] = append(sources.sourceFiles[file.Path], file.Text)
			}
		}
	}
	sort.Strings(sources.paths)
	return sources, nil
}

func (sources *sourceSet) PackagePaths() ([]string, error) {
	return append([]string(nil), sources.paths...), nil
}

func (sources *sourceSet) Package(modulePath string) (workspace.SourcePackage, bool, error) {
	modulePath = strings.TrimSpace(modulePath)
	input, exists := sources.inputs[modulePath]
	if !exists {
		return workspace.SourcePackage{}, false, nil
	}
	loaded, exists := sources.loaded[modulePath]
	if !exists {
		pkg := workspace.SourcePackage{
			ID:               workspace.PackageID{Namespace: input.Namespace, Path: input.PackagePath},
			ModulePath:       modulePath,
			SelectionTarget:  input.SelectionTarget,
			SourceCandidates: append([]workspace.SourceCandidate(nil), input.SourceCandidates...),
		}
		sourceFiles := make(map[string]string, len(input.Files)+len(input.TestFiles))
		pkg.Files = make([]source.File, len(input.Files))
		for i, file := range input.Files {
			if _, duplicate := sourceFiles[file.Path]; duplicate {
				return workspace.SourcePackage{}, false, fmt.Errorf("duplicate source path %q in package %q", file.Path, modulePath)
			}
			sourceFiles[file.Path] = file.Text
			pkg.Files[i] = source.File{Path: file.Path, Text: file.Text, OriginPath: file.OriginPath}
		}
		pkg.TestFiles = make([]source.File, len(input.TestFiles))
		for i, file := range input.TestFiles {
			if _, duplicate := sourceFiles[file.Path]; duplicate {
				return workspace.SourcePackage{}, false, fmt.Errorf("duplicate source path %q in package %q", file.Path, modulePath)
			}
			sourceFiles[file.Path] = file.Text
			pkg.TestFiles[i] = source.File{Path: file.Path, Text: file.Text, OriginPath: file.OriginPath}
		}
		pkg.Resources = make([]workspace.ResourceFile, len(input.Resources))
		for i, resource := range input.Resources {
			data := resource.Data
			if resource.SourcePath != "" {
				if resource.Data != nil {
					return workspace.SourcePackage{}, false, fmt.Errorf("resource %q in package %q specifies source path and data", resource.Path, modulePath)
				}
				text, found := sourceFiles[resource.SourcePath]
				if !found {
					matches := sources.sourceFiles[resource.SourcePath]
					if len(matches) > 1 {
						return workspace.SourcePackage{}, false, fmt.Errorf("resource %q in package %q references ambiguous source %q", resource.Path, modulePath, resource.SourcePath)
					}
					if len(matches) == 1 {
						text, found = matches[0], true
					}
				}
				if !found {
					return workspace.SourcePackage{}, false, fmt.Errorf("resource %q in package %q references missing source %q", resource.Path, modulePath, resource.SourcePath)
				}
				data = []byte(text)
			}
			pkg.Resources[i] = workspace.ResourceFile{Path: resource.Path, Data: append([]byte(nil), data...)}
		}
		var err error
		loaded, err = workspace.NewMemorySourceSet([]workspace.SourcePackage{pkg})
		if err != nil {
			return workspace.SourcePackage{}, false, err
		}
		sources.loaded[modulePath] = loaded
	}
	return loaded.Package(modulePath)
}
