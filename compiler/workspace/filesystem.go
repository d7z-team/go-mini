package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// DiscoverSourceTree maps .mgo sources and resources below one module path.
func DiscoverSourceTree(fsys fs.FS, root, modulePath string) (SourceSet, error) {
	if fsys == nil {
		return nil, errors.New("nil source filesystem")
	}
	root, err := normalizeFSRoot(root)
	if err != nil {
		return nil, err
	}
	tree, err := fs.Sub(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("open source root %q: %w", root, err)
	}
	var files []TreeFile
	err = walkSourceFiles(tree, func(name string) error {
		data, err := fs.ReadFile(tree, name)
		if err != nil {
			return fmt.Errorf("read source %q: %w", name, err)
		}
		files = append(files, TreeFile{Path: name, Data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewTreeSourceSet(modulePath, files)
}

func normalizeFSRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return ".", nil
	}
	if root != "." && !fs.ValidPath(root) {
		return "", fmt.Errorf("invalid filesystem root %q", root)
	}
	return root, nil
}

// walkSourceFiles shares filesystem boundaries between eager and indexed discovery.
func walkSourceFiles(fsys fs.FS, visit func(string) error) error {
	return fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
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
		return visit(name)
	})
}
