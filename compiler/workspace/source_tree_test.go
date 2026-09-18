package workspace

import (
	"bytes"
	"fmt"
	"testing"
)

func TestTreeSourceBytesAreOwnedBySnapshot(t *testing.T) {
	code := []byte("package app\n")
	data := []byte{0, 255, 1}
	set, err := NewTreeSourceSet("app", []TreeFile{{Path: "main.mgo", Data: code}, {Path: "data.bin", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	code[0], data[0] = 'X', 42
	for range 2 {
		pkg, found, err := set.Package("app")
		if err != nil || !found {
			t.Fatalf("package: %v %v", found, err)
		}
		if pkg.Files[0].Text != "package app\n" || !bytes.Equal(pkg.Resources[0].Data, []byte{0, 255, 1}) {
			t.Fatalf("snapshot changed: %+v", pkg)
		}
		pkg.Resources[0].Data[0] = 99
		pkg.Files[0].Text = "changed"
	}
}

func BenchmarkTreeSourceResources(b *testing.B) {
	files := []TreeFile{{Path: "main.mgo", Text: "package app\n"}}
	for i := range 32 {
		files = append(files,
			TreeFile{Path: fmt.Sprintf("p%d/main.mgo", i), Text: "package p\n"},
			TreeFile{Path: fmt.Sprintf("p%d/data.bin", i), Data: bytes.Repeat([]byte{byte(i)}, 64<<10)},
		)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewTreeSourceSet("app", files); err != nil {
			b.Fatal(err)
		}
	}
}
