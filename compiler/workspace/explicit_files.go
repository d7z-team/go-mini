package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

// CommandLinePackage is the import path assigned to named source files.
const CommandLinePackage = "command-line-arguments"

// NewExplicitSourceSet builds a command-line package from named production
// source files. The filesystem is rooted at their common directory and is read
// only for resources selected by //go:embed.
func NewExplicitSourceSet(filesystem fs.FS, sourceFiles []source.File, buildTarget target.Target) (SourceSet, error) {
	if len(sourceFiles) == 0 {
		return nil, errors.New("explicit source file list is empty")
	}
	files := append([]source.File(nil), sourceFiles...)
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !fs.ValidPath(file.Path) || path.Base(file.Path) != file.Path {
			return nil, fmt.Errorf("explicit source file path %q must be a base name", file.Path)
		}
		if _, exists := seen[file.Path]; exists {
			return nil, fmt.Errorf("duplicate explicit source file %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		if !isSourceFile(file.Path) {
			return nil, fmt.Errorf("explicit source file %q must use .mgo", file.Path)
		}
		if isTestFile(file.Path) {
			return nil, fmt.Errorf("explicit source file %q is a test file", file.Path)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	patterns := make([]string, 0)
	for _, file := range files {
		if file.OriginPath != "" {
			file.Path = file.OriginPath
		}
		document := parser.ParseDocumentFile(CommandLinePackage, file)
		for _, parsedFile := range document.Program.Files {
			for _, declaration := range parsedFile.Decls {
				if declaration.Kind == ast.DeclVar {
					patterns = append(patterns, declaration.Var.EmbedPatterns...)
				}
			}
		}
	}
	resources, err := loadExplicitResources(filesystem, patterns)
	if err != nil {
		return nil, err
	}
	normalizedTarget, err := target.Normalize(buildTarget)
	if err != nil {
		return nil, err
	}
	candidates := make([]SourceCandidate, len(files))
	for index, file := range files {
		hash := source.HashText(file.Text)
		if file.OriginPath != "" {
			hash = source.HashText(file.OriginPath + "\x00" + hash)
		}
		candidates[index] = SourceCandidate{Path: file.Path, Hash: hash, Size: len(file.Text), Selected: true}
	}
	sources, err := NewMemorySourceSet([]SourcePackage{{
		ID:               PackageID{Namespace: "command", Path: "files"},
		ModulePath:       CommandLinePackage,
		Files:            files,
		Resources:        resources,
		SelectionTarget:  normalizedTarget,
		SourceCandidates: candidates,
	}})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

func loadExplicitResources(tree fs.FS, patterns []string) ([]ResourceFile, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	if tree == nil {
		return nil, errors.New("explicit source filesystem is required for //go:embed")
	}
	var candidates []ResourceFile
	err := fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".hg", ".svn", ".bzr":
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || strings.HasSuffix(name, ".mrpc") {
			return nil
		}
		candidates = append(candidates, ResourceFile{Path: name})
		return nil
	})
	if err != nil {
		return nil, err
	}
	matchedByPath := make(map[string]ResourceFile)
	for _, pattern := range patterns {
		matched, matchErr := matchEmbedResources([]string{pattern}, candidates)
		if matchErr != nil {
			// The compiler owns stable embed diagnostics. Skipping only the bad
			// pattern preserves resources selected by the remaining directives.
			continue
		}
		for _, resource := range matched {
			matchedByPath[resource.Path] = resource
		}
	}
	matched := make([]ResourceFile, 0, len(matchedByPath))
	for _, resource := range matchedByPath {
		matched = append(matched, resource)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Path < matched[j].Path })
	for index := range matched {
		data, readErr := fs.ReadFile(tree, matched[index].Path)
		if readErr != nil {
			return nil, fmt.Errorf("read embedded resource %q: %w", matched[index].Path, readErr)
		}
		matched[index].Data = data
		matched[index].Hash = source.HashText(string(data))
	}
	return matched, nil
}
