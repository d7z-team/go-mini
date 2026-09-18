package language

import "testing"

func TestLineIndexUTF16RoundTrip(t *testing.T) {
	index := NewLineIndex("a😀é\r\nnext")
	positions := []struct {
		offset int
		want   Position
	}{{0, Position{0, 0}}, {1, Position{0, 1}}, {5, Position{0, 3}}, {8, Position{0, 5}}, {10, Position{1, 0}}, {14, Position{1, 4}}}
	for _, test := range positions {
		position, err := index.Position(test.offset)
		if err != nil || position != test.want {
			t.Fatalf("Position(%d) = %+v, %v; want %+v", test.offset, position, err, test.want)
		}
		offset, err := index.Offset(position)
		if err != nil || offset != test.offset {
			t.Fatalf("Offset(%+v) = %d, %v; want %d", position, offset, err, test.offset)
		}
	}
	if _, err := index.Offset(Position{Line: 0, Character: 2}); err == nil {
		t.Fatal("position inside surrogate pair accepted")
	}
}

func TestDocumentStoreAppliesSequentialChangesAndRejectsRollback(t *testing.T) {
	store := NewDocumentStore(DefaultLimits())
	id := DocumentIdentity{URI: "file:///main.mgo", ModulePath: "example", Path: "main.mgo"}
	if err := store.Open(id, 1, "abc"); err != nil {
		t.Fatal(err)
	}
	changes := []ContentChange{
		{Range: &Range{Start: Position{0, 1}, End: Position{0, 2}}, Text: "XY"},
		{Range: &Range{Start: Position{0, 3}, End: Position{0, 4}}, Text: "Z"},
	}
	if err := store.Change(id.URI, 2, changes); err != nil {
		t.Fatal(err)
	}
	document, _ := store.Document(id.URI)
	if document.Text != "aXYZ" {
		t.Fatalf("changed text = %q", document.Text)
	}
	if err := store.Change(id.URI, 2, nil); err != ErrVersionRollback {
		t.Fatalf("rollback error = %v", err)
	}
}
