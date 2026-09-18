package cache

import (
	"bytes"
	"strconv"
	"testing"
)

func TestMemoryBackendBoundsAndClear(t *testing.T) {
	b := NewMemoryBackendWithConfig(MemoryConfig{MaxEntries: 8, MaxBytes: 1024})
	for i := range 1000 {
		data := []byte(strconv.Itoa(i))
		id := OutputIDFor(data)
		if err := b.PutOutput(id, data); err != nil {
			t.Fatal(err)
		}
		if err := b.PutAction(ActionID(id), Entry{Output: id, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		stats := b.Stats()
		if stats.Entries > 8 || stats.Bytes > 1024 {
			t.Fatalf("budget exceeded: %+v", stats)
		}
	}
	if b.Stats().Evictions == 0 {
		t.Fatal("churn did not evict entries")
	}
	large := bytes.Repeat([]byte("x"), 1024)
	id := OutputIDFor(large)
	if err := b.PutOutput(id, large); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := b.GetOutput(id); found {
		t.Fatal("oversized output retained")
	}
	b.Clear()
	if stats := b.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatalf("Clear retained data: %+v", stats)
	}
	if err := b.PutOutput(OutputIDFor(nil), nil); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := b.GetOutput(OutputIDFor(nil)); !found {
		t.Fatal("store after Clear missed")
	}
}

func TestMemoryBackendFIFOAndUpdates(t *testing.T) {
	b := NewMemoryBackendWithConfig(MemoryConfig{MaxEntries: 2})
	first, second, third := []byte("first"), []byte("second"), []byte("third")
	for _, data := range [][]byte{first, second, first, third} {
		if err := b.PutOutput(OutputIDFor(data), data); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, _ := b.GetOutput(OutputIDFor(first)); found {
		t.Fatal("oldest entry was not evicted")
	}
	for _, data := range [][]byte{second, third} {
		if value, found, _ := b.GetOutput(OutputIDFor(data)); !found || !bytes.Equal(value, data) {
			t.Fatal("FIFO lost recent output")
		}
	}
	action := ActionID(OutputIDFor(first))
	b.Clear()
	for i := range 100 {
		if err := b.PutAction(action, Entry{Size: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(b.order) != 1 || b.Stats().Entries != 1 {
		t.Fatal("overwrite appended retention state")
	}
	entry, found, err := b.GetAction(action)
	if err != nil || !found || entry.Size != 99 {
		t.Fatalf("updated action = %+v, %v, %v", entry, found, err)
	}
}
