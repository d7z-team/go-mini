package source

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestFilePositionMapsOffsetToLineColumn(t *testing.T) {
	file := NewFile("file.main", "main.mgo", "one\ntwo\n")

	pos, ok := file.Position(5)
	if !ok {
		t.Fatal("expected position")
	}
	if pos.File != "main.mgo" || pos.Offset != 5 || pos.Line != 2 || pos.Column != 1 {
		t.Fatalf("unexpected position: %#v", pos)
	}
	end, ok := file.Position(len(file.Text))
	if !ok {
		t.Fatal("expected EOF position")
	}
	if end.Line != 3 || end.Column != 0 {
		t.Fatalf("unexpected EOF position: %#v", end)
	}
}

func TestFilePositionUsesUTF8ByteColumns(t *testing.T) {
	file := NewFile("file.0", "main.mgo", "a\n界b\n")
	pos, ok := file.Position(3)
	if !ok || pos.Line != 2 || pos.Column != 1 {
		t.Fatalf("Position(3) = %+v, ok=%v", pos, ok)
	}
	if file.Hash != HashText(file.Text) || len(file.LineStarts) != 3 {
		t.Fatalf("file source metadata was not initialized: %+v", file)
	}
}

func TestNormalizeFileInitializesDerivedMetadata(t *testing.T) {
	file := NormalizeFile(File{
		ID: "file.0", Path: "main.mgo", Text: "one\ntwo\n",
		Hash: "stale", LineStarts: []int{0, 99},
	})
	if file.Hash != HashText(file.Text) || len(file.LineStarts) != 3 {
		t.Fatalf("NormalizeFile() = %+v", file)
	}
	position, ok := file.Position(5)
	if !ok || position.Line != 2 || position.Column != 1 {
		t.Fatalf("Position(5) = %+v, ok=%v", position, ok)
	}
}

func TestInitializeFileReusesDerivedMetadata(t *testing.T) {
	file := InitializeFile(File{
		ID: "file.0", Path: "main.mgo", Text: "one\ntwo\n",
		Hash: "known", LineStarts: []int{0, 4, 8},
	})
	if file.Hash != "known" || len(file.LineStarts) != 3 || file.LineStarts[1] != 4 {
		t.Fatalf("InitializeFile() = %+v", file)
	}

	missing := InitializeFile(File{ID: "file.1", Path: "other.mgo", Text: "one\ntwo\n"})
	if missing.Hash != HashText(missing.Text) || len(missing.LineStarts) != 3 {
		t.Fatalf("InitializeFile() did not fill metadata: %+v", missing)
	}
}

func TestFileSpanValidatesRange(t *testing.T) {
	file := File{Path: "main.mgo", Text: "abc\ndef"}

	span, ok := file.Span(1, 5)
	if !ok {
		t.Fatal("expected span")
	}
	if !span.Valid() {
		t.Fatalf("expected valid span: %#v", span)
	}
	if !span.ContainsOffset(1) || !span.ContainsOffset(4) || span.ContainsOffset(5) {
		t.Fatalf("unexpected span containment: %#v", span)
	}
	if _, ok := file.Span(5, 1); ok {
		t.Fatal("expected reversed span to be rejected")
	}
}

func TestFileSetLookup(t *testing.T) {
	files := NewFileSet([]File{{ID: "file.main", Path: "main.mgo"}})
	files.AddFile(File{ID: "file.lib", Path: "lib.mgo"})

	if file, ok := files.FileByID("file.lib"); !ok || file.Path != "lib.mgo" {
		t.Fatalf("unexpected id lookup: %#v, ok=%v", file, ok)
	}
	if file, ok := files.FileByPath("main.mgo"); !ok || file.ID != "file.main" {
		t.Fatalf("unexpected path lookup: %#v, ok=%v", file, ok)
	}
	if _, ok := files.FileByPath("missing.mgo"); ok {
		t.Fatal("expected missing path lookup")
	}
}

func TestHashTextMatchesSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("package main\n"))
	want := hex.EncodeToString(sum[:])
	if got := HashText("package main\n"); got != want {
		t.Fatalf("expected hash %q, got %q", want, got)
	}
}

func TestHashFilesUsesStableFileMetadata(t *testing.T) {
	files := []File{{
		ID:   "file.main",
		Path: "main.mgo",
		Text: "package main\n",
	}}
	left := HashFiles(files)
	right := HashFiles(files)
	if left == "" || left != right {
		t.Fatalf("expected stable non-empty file set hash, got %q and %q", left, right)
	}
	files[0].Text = "package changed\n"
	if changed := HashFiles(files); changed == left {
		t.Fatalf("expected changed source hash, got %q", changed)
	}
}
