package language

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func FuzzLineIndexRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "\r", "ascii\nnext", "中文😀\r\nend", "é"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) || len(text) > 64<<10 {
			return
		}
		index := NewLineIndex(text)
		for offset := 0; offset <= len(text); offset++ {
			position, err := index.Position(offset)
			if err != nil {
				continue
			}
			got, err := index.Offset(position)
			if err != nil || got > offset {
				t.Fatalf("round trip %d -> %+v -> %d, %v", offset, position, got, err)
			}
			canonical, err := index.Position(got)
			if err != nil || canonical != position {
				t.Fatalf("canonical round trip %+v -> %d -> %+v, %v", position, got, canonical, err)
			}
		}
	})
}

func FuzzEngineEditSequence(f *testing.F) {
	f.Add([]byte("package main\nfunc main() {}\n"))
	f.Add([]byte("package main\x00func broken( {"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
			ModulePath: "fuzz/main",
			Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		limits := Limits{MaxFileBytes: 32 << 10, MaxFiles: 4, MaxDiagnostics: 16}
		engine, err := NewEngine(Config{Root: "fuzz/main", Sources: sources, Limits: limits})
		if err != nil {
			t.Fatal(err)
		}
		identity := DocumentIdentity{URI: "mini-go://fuzz/main/main.mgo", ModulePath: "fuzz/main", Path: "main.mgo"}
		if err := engine.Open(identity, 1, "package main\nfunc main() {}\n"); err != nil {
			t.Fatal(err)
		}
		version := 2
		for _, replacement := range bytes.Split(data, []byte{0}) {
			if err := engine.Change(identity.URI, version, []ContentChange{{Text: string(replacement)}}); err != nil {
				t.Fatal(err)
			}
			version++
			if diagnostics := engine.Snapshot().diagnostics[identity.URI]; len(diagnostics) > limits.MaxDiagnostics {
				t.Fatalf("LSP diagnostic budget exceeded: %d", len(diagnostics))
			}
		}
	})
}

func FuzzDocumentChanges(f *testing.F) {
	f.Add("package main\n", "main", 0, 7)
	f.Add("000000\r\n", "0", 0, 7)
	f.Fuzz(func(t *testing.T, source, replacement string, start, end int) {
		if !utf8.ValidString(source) || !utf8.ValidString(replacement) || len(source)+len(replacement) > 64<<10 || start < 0 || end < start || end > len(source) {
			return
		}
		index := NewLineIndex(source)
		left, leftErr := index.Position(start)
		right, rightErr := index.Position(end)
		if leftErr != nil || rightErr != nil {
			return
		}
		canonicalStart, err := index.Offset(left)
		if err != nil {
			t.Fatal(err)
		}
		canonicalEnd, err := index.Offset(right)
		if err != nil {
			t.Fatal(err)
		}
		store := NewDocumentStore(DefaultLimits())
		identity := DocumentIdentity{URI: "file:///f.mgo", ModulePath: "f", Path: "f.mgo"}
		if err := store.Open(identity, 1, source); err != nil {
			t.Fatal(err)
		}
		if err := store.Change(identity.URI, 2, []ContentChange{{Range: &Range{Start: left, End: right}, Text: replacement}}); err != nil {
			t.Fatal(err)
		}
		document, _ := store.Document(identity.URI)
		if document.Text != source[:canonicalStart]+replacement+source[canonicalEnd:] {
			t.Fatal("incremental change produced unexpected text")
		}
	})
}
