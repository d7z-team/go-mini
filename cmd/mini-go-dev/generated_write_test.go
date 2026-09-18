package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratedGzipPreservesBinaryContentAndDeterministicEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json.gz")
	input := []byte("<>&\x00\xff\n")
	if err := writeGeneratedGzip(path, input); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGeneratedGzip(path, input); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("gzip generation is not deterministic")
	}
	reader, err := gzip.NewReader(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, input) {
		t.Fatalf("decoded bundle = %q", output)
	}
}

func TestWriteAtomicallyReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "generated.go")
	if err := writeGeneratedFile(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeGeneratedFile(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second\n" {
		t.Fatalf("generated content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("generated mode = %o", info.Mode().Perm())
	}
}

func TestWriteKeepsUnchangedFileTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	data := []byte("same\n")
	if err := writeGeneratedFile(path, data); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := writeGeneratedFile(path, data); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatalf("unchanged generated file time = %v, want %v", info.ModTime(), old)
	}
}

func TestWriteReportsInvalidParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGeneratedFile(filepath.Join(parent, "generated.go"), []byte("data")); err == nil {
		t.Fatal("write with a file parent succeeded")
	}
}
