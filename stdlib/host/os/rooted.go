package oshost

import (
	"io/fs"
	"os"
	"sync"
	"time"
)

// RootedFilesystem exposes real OS files while confining paths to one os.Root.
type RootedFilesystem struct {
	mu             sync.RWMutex
	root           *os.Root
	workingDir     string
	cacheDirectory string
}

func NewRootedFilesystem(root string) (*RootedFilesystem, error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &RootedFilesystem{root: opened, workingDir: ".", cacheDirectory: ".cache"}, nil
}

func (filesystem *RootedFilesystem) Close() error {
	if filesystem == nil {
		return nil
	}
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.root == nil {
		return nil
	}
	err := filesystem.root.Close()
	filesystem.root = nil
	return err
}

func (filesystem *RootedFilesystem) Open(name string, flag int, perm fs.FileMode) (File, error) {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return root.OpenFile(name, flag, perm)
}

func (filesystem *RootedFilesystem) Stat(name string) (fs.FileInfo, error) {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return root.Stat(name)
}

func (filesystem *RootedFilesystem) Lstat(name string) (fs.FileInfo, error) {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return root.Lstat(name)
}

func (filesystem *RootedFilesystem) ReadDir(name string) ([]fs.DirEntry, error) {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return fs.ReadDir(root.FS(), name)
}

func (filesystem *RootedFilesystem) Mkdir(name string, perm fs.FileMode) error {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return err
	}
	defer unlock()
	return root.Mkdir(name, perm)
}

func (filesystem *RootedFilesystem) Remove(name string) error {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return err
	}
	defer unlock()
	return root.Remove(name)
}

func (filesystem *RootedFilesystem) Rename(oldName, newName string) error {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return err
	}
	defer unlock()
	return root.Rename(oldName, newName)
}

func (filesystem *RootedFilesystem) Readlink(name string) (string, error) {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return "", err
	}
	defer unlock()
	return root.Readlink(name)
}

func (filesystem *RootedFilesystem) Chtimes(name string, atime, mtime time.Time) error {
	root, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return err
	}
	defer unlock()
	return root.Chtimes(name, atime, mtime)
}

func (filesystem *RootedFilesystem) Getwd() (string, error) {
	_, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return "", err
	}
	defer unlock()
	return filesystem.workingDir, nil
}

func (filesystem *RootedFilesystem) UserCacheDir() (string, error) {
	_, unlock, err := filesystem.lockedRoot()
	if err != nil {
		return "", err
	}
	defer unlock()
	return filesystem.cacheDirectory, nil
}

func (filesystem *RootedFilesystem) lockedRoot() (*os.Root, func(), error) {
	if filesystem == nil {
		return nil, func() {}, fs.ErrClosed
	}
	filesystem.mu.RLock()
	if filesystem.root == nil {
		filesystem.mu.RUnlock()
		return nil, func() {}, fs.ErrClosed
	}
	return filesystem.root, filesystem.mu.RUnlock, nil
}
