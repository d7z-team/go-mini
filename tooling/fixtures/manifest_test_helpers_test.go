package fixtures

import (
	"io/fs"
	"testing/fstest"
)

type trackedFixtureFS struct {
	fstest.MapFS
	readErr error
	open    int
}

func (root *trackedFixtureFS) Open(name string) (fs.File, error) {
	file, err := root.MapFS.Open(name)
	if err != nil {
		return nil, err
	}
	root.open++
	return &trackedFixtureFile{File: file, owner: root}, nil
}

type trackedFixtureFile struct {
	fs.File
	owner *trackedFixtureFS
}

func (file *trackedFixtureFile) Read(data []byte) (int, error) {
	if file.owner.readErr != nil {
		return 0, file.owner.readErr
	}
	return file.File.Read(data)
}

func (file *trackedFixtureFile) Close() error {
	file.owner.open--
	return file.File.Close()
}
