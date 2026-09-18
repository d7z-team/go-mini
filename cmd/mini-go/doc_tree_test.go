package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	docgen "github.com/d7z-team/mini-go/tooling/doc"
)

func TestCompareMarkdownTree(t *testing.T) {
	for _, mode := range []string{"equal", "missing", "content", "unexpected"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			files := []docgen.File{{Path: "index.md", Text: []byte("index\n")}, {Path: "nested/item.md", Text: []byte("item\n")}}
			for _, file := range files {
				name := filepath.Join(root, filepath.FromSlash(file.Path))
				if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, file.Text, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "missing":
				if err := os.Remove(filepath.Join(root, "index.md")); err != nil {
					t.Fatal(err)
				}
			case "content":
				if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("changed"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "unexpected":
				if err := os.WriteFile(filepath.Join(root, "extra.md"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := compareMarkdownTree(root, files)
			if mode == "equal" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), mode) {
				t.Fatalf("comparison: %v, want %s", err, mode)
			}
		})
	}
}

func BenchmarkCompareMarkdownTree(b *testing.B) {
	root := b.TempDir()
	files := make([]docgen.File, 32)
	for i := range files {
		files[i] = docgen.File{Path: fmt.Sprintf("package%d.md", i), Text: []byte(strings.Repeat("package documentation\n", 4096))}
		if err := os.WriteFile(filepath.Join(root, files[i].Path), files[i].Text, 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := compareMarkdownTree(root, files); err != nil {
			b.Fatal(err)
		}
	}
}
