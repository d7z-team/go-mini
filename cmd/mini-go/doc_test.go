package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocCommandRegeneratesAndChecksMarkdownTree(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "value.mgo"), []byte("// Package example is documented.\npackage example\n// Value returns one.\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"-C", directory, "doc", "-module", "example", "-out", "reference"}
	if err := runCLI(args, &stdout, &stderr); err != nil {
		t.Fatalf("generate docs: %v stderr=%s", err, stderr.String())
	}
	page := filepath.Join(directory, "reference", "packages", "example.md")
	data, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(data), "func Value() int") {
		t.Fatalf("generated page=%q err=%v", data, err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI([]string{"-C", directory, "doc", "-module", "example", "-out", "reference", "-check"}, &stdout, &stderr); err != nil {
		t.Fatalf("check docs: %v stderr=%s", err, stderr.String())
	}
	if err := os.WriteFile(page, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI([]string{"-C", directory, "doc", "-module", "example", "-out", "reference", "-check", "./..."}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale check error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "reference", "extra.md"), []byte("extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI(args, &stdout, &stderr); err != nil {
		t.Fatalf("regenerate docs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "reference", "extra.md")); !os.IsNotExist(err) {
		t.Fatalf("stale generated file remains: %v", err)
	}
	before, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "value.mgo"), []byte("package example\nfunc Broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCLI(args, &stdout, &stderr); err == nil {
		t.Fatal("invalid source unexpectedly generated documentation")
	}
	after, err := os.ReadFile(page)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed generation changed existing output: err=%v before=%q after=%q", err, before, after)
	}
}

func TestDocCommandSelectsExactPackageAndDependencies(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "root.mgo"), []byte("package example\nfunc Root() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dependency := filepath.Join(directory, "dependency")
	selected := filepath.Join(directory, "selected")
	if err := os.MkdirAll(dependency, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(selected, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "dependency.mgo"), []byte("package dependency\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selected, "selected.mgo"), []byte("package selected\nimport \"example/dependency\"\nfunc Value() int { return dependency.Value() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runCLI([]string{"-C", directory, "doc", "-module", "example", "-out", "reference", "./selected"}, &stdout, &stderr); err != nil {
		t.Fatalf("generate selected package: %v stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(directory, "reference", "packages", "example", "selected.md")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"example.md", filepath.Join("example", "dependency.md")} {
		if _, err := os.Stat(filepath.Join(directory, "reference", "packages", name)); !os.IsNotExist(err) {
			t.Fatalf("unselected package page %q exists: %v", name, err)
		}
	}
}
