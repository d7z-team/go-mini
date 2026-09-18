package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func loadExplicitSourceFiles(workDir string, names []string, buildTarget target.Target) (workspace.SourceSet, string, error) {
	if len(names) == 0 {
		return nil, "", errors.New("explicit source file list is empty")
	}
	files := make([]source.File, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	directory := ""
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, "", errors.New("explicit source file path is empty")
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(workDir, name)
		}
		absolute, err := filepath.Abs(name)
		if err != nil {
			return nil, "", err
		}
		absolute = filepath.Clean(absolute)
		if _, exists := seen[absolute]; exists {
			return nil, "", fmt.Errorf("duplicate explicit source file %q", absolute)
		}
		seen[absolute] = struct{}{}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, "", err
		}
		if !info.Mode().IsRegular() {
			return nil, "", fmt.Errorf("explicit source file %q is not a regular file", absolute)
		}
		fileDirectory := filepath.Dir(absolute)
		if directory == "" {
			directory = fileDirectory
		} else if directory != fileDirectory {
			return nil, "", errors.New("explicit source files must be in one directory")
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return nil, "", err
		}
		files = append(files, source.File{Path: filepath.Base(absolute), Text: string(data)})
	}
	sources, err := workspace.NewExplicitSourceSet(os.DirFS(directory), files, buildTarget)
	if err != nil {
		return nil, "", err
	}
	return sources, directory, nil
}
