//go:build !minigo

package workspace

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// SelectPackages resolves Go-style package patterns relative to workDir.
// The source set should contain only packages that are valid command targets.
func SelectPackages(moduleRoot, workDir, modulePath string, sources SourceSet, patterns []string) ([]string, error) {
	moduleRoot, err := filepath.Abs(moduleRoot)
	if err != nil {
		return nil, err
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(moduleRoot, workDir)
	if err != nil {
		return nil, err
	}
	relative = filepath.ToSlash(relative)
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return nil, fmt.Errorf("working directory %q is outside module root %q", workDir, moduleRoot)
	}
	current := strings.TrimSuffix(modulePath, "/")
	if relative != "." {
		current += "/" + relative
	}
	available, err := sources.PackagePaths()
	if err != nil {
		return nil, err
	}
	availableSet := make(map[string]struct{}, len(available))
	for _, packagePath := range available {
		availableSet[packagePath] = struct{}{}
	}
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	selected := make(map[string]struct{})
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return nil, errors.New("package pattern is empty")
		}
		recursive := pattern == "./..." || strings.HasSuffix(pattern, "/...")
		var candidate string
		switch {
		case pattern == "." || pattern == "./...":
			candidate = current
		case strings.HasPrefix(pattern, "./"):
			rel := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/...")
			rel = path.Clean(rel)
			if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
				return nil, fmt.Errorf("invalid package pattern %q", pattern)
			}
			candidate = current + "/" + rel
		default:
			candidate = strings.TrimSuffix(pattern, "/...")
		}
		candidate = strings.TrimSuffix(candidate, "/")
		if recursive {
			for _, packagePath := range available {
				if packagePath == candidate || strings.HasPrefix(packagePath, candidate+"/") {
					selected[packagePath] = struct{}{}
				}
			}
			continue
		}
		if _, ok := availableSet[candidate]; !ok {
			return nil, fmt.Errorf("package %q is not available", candidate)
		}
		selected[candidate] = struct{}{}
	}
	if len(selected) == 0 {
		return nil, errors.New("package patterns matched no packages")
	}
	packages := make([]string, 0, len(selected))
	for packagePath := range selected {
		packages = append(packages, packagePath)
	}
	sort.Strings(packages)
	return packages, nil
}
