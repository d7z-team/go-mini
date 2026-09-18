package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicFileWrites(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "output")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if batch {
				err = writeFilesAtomically([]atomicFile{{path: path, data: []byte("updated"), mode: 0o600}})
			} else {
				err = writeFileAtomically(path, []byte("updated"), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "updated" {
				t.Fatalf("output = %q, %v", data, err)
			}
			after, err := os.Stat(path)
			if err != nil || after.Mode().Perm() != before.Mode().Perm() {
				t.Fatalf("output permissions changed: %v, %v", after, err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != "output" {
				t.Fatalf("output directory = %v, %v", entries, err)
			}
		})
	}
}

func TestAtomicFilePreparationFailure(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		name := "directory"
		if duplicate {
			name = "duplicate"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "output")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			second := directory
			if duplicate {
				second = path
			}
			err := writeFilesAtomically([]atomicFile{
				{path: path, data: []byte("updated"), mode: 0o600},
				{path: second, data: []byte("second"), mode: 0o600},
			})
			if err == nil {
				t.Fatal("invalid output destination succeeded")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "original" {
				t.Fatalf("original output = %q, %v", data, err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != "output" {
				t.Fatalf("temporary files retained: %v, %v", entries, err)
			}
		})
	}
}

func TestAtomicFileCommitFailure(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(destination, []byte("data"), 0o600); err == nil {
		t.Fatal("replacing a directory with a file succeeded")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "destination" || !entries[0].IsDir() {
		t.Fatalf("failed commit changed destination or retained temporary files: %v, %v", entries, err)
	}
}

func TestAtomicBatchRestoresFirstFileWhenSecondCommitFails(t *testing.T) {
	for _, existed := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existed], func(t *testing.T) {
			directory := t.TempDir()
			first := filepath.Join(directory, "binding.mgo")
			second := filepath.Join(directory, "binding.rs")
			if existed {
				if err := os.WriteFile(first, []byte("original module"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(second, 0o700); err != nil {
				t.Fatal(err)
			}
			prepared := []preparedAtomicFile{}
			for _, path := range []string{first, second} {
				file := atomicFile{path: path, data: []byte("replacement"), mode: 0o600}
				temporary, err := prepareAtomicFile(file)
				if err != nil {
					t.Fatal(err)
				}
				defer os.Remove(temporary)
				prepared = append(prepared, preparedAtomicFile{atomicFile: file, temporary: temporary})
			}
			prepared[0].existed, prepared[0].oldData, prepared[0].oldMode = existed, []byte("original module"), 0o640
			if err := commitAtomicFiles(prepared); err == nil {
				t.Fatal("second rename unexpectedly succeeded")
			}
			data, err := os.ReadFile(first)
			if existed {
				if err != nil || string(data) != "original module" {
					t.Fatalf("first file not restored: %q, %v", data, err)
				}
				info, err := os.Stat(first)
				if err != nil || info.Mode().Perm() != 0o640 {
					t.Fatalf("permissions not restored: %v, %v", info, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("new first file not rolled back: %v", err)
			}
			info, err := os.Stat(second)
			if err != nil || !info.IsDir() {
				t.Fatalf("failed destination changed: %v, %v", info, err)
			}
		})
	}
}
