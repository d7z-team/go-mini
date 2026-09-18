package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type atomicFile struct {
	path string
	data []byte
	mode fs.FileMode
}

type preparedAtomicFile struct {
	atomicFile
	temporary string
	existed   bool
	oldData   []byte
	oldMode   fs.FileMode
}

func writeFileAtomically(path string, data []byte, mode fs.FileMode) error {
	name, err := prepareAtomicFile(atomicFile{path: path, data: data, mode: mode})
	if err != nil {
		return err
	}
	defer os.Remove(name)
	return os.Rename(name, path)
}

func prepareAtomicFile(file atomicFile) (name string, err error) {
	directory := filepath.Dir(file.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(directory, ".mini-go-write-*.tmp")
	if err != nil {
		return "", err
	}
	name = temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(file.mode.Perm()); err != nil {
		return name, err
	}
	if _, err := temporary.Write(file.data); err != nil {
		return name, err
	}
	if err := temporary.Sync(); err != nil {
		return name, err
	}
	return name, temporary.Close()
}

func writeFilesAtomically(files []atomicFile) error {
	seen := make(map[string]struct{}, len(files))
	prepared := make([]preparedAtomicFile, 0, len(files))
	defer func() {
		for _, file := range prepared {
			if file.temporary != "" {
				_ = os.Remove(file.temporary)
			}
		}
	}()
	for _, file := range files {
		file.path = filepath.Clean(file.path)
		if _, duplicate := seen[file.path]; duplicate {
			return fmt.Errorf("duplicate output path %q", file.path)
		}
		seen[file.path] = struct{}{}
		item := preparedAtomicFile{atomicFile: file}
		if info, err := os.Stat(file.path); err == nil {
			item.existed = true
			item.oldMode = info.Mode().Perm()
			item.oldData, err = os.ReadFile(file.path)
			if err != nil {
				return err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		temporary, err := prepareAtomicFile(file)
		if err != nil {
			return err
		}
		item.temporary = temporary
		prepared = append(prepared, item)
	}
	return commitAtomicFiles(prepared)
}

// commitAtomicFiles restores already replaced destinations if a later rename fails.
func commitAtomicFiles(prepared []preparedAtomicFile) error {
	committed := 0
	for index := range prepared {
		if err := os.Rename(prepared[index].temporary, prepared[index].path); err != nil {
			var rollbackErr error
			for rollback := committed - 1; rollback >= 0; rollback-- {
				old := prepared[rollback]
				if old.existed {
					rollbackErr = errors.Join(rollbackErr, writeFileAtomically(old.path, old.oldData, old.oldMode))
				} else {
					rollbackErr = errors.Join(rollbackErr, os.Remove(old.path))
				}
			}
			return errors.Join(err, rollbackErr)
		}
		prepared[index].temporary = ""
		committed++
	}
	return nil
}
