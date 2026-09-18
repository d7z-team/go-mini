package minigo_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestEngineCompilesEmbeddedResources(t *testing.T) {
	mainSource := `package main
import (
	embedded "embed"
	"io/fs"
)

type Text string
type Octet byte
type Data []Octet
type Assets = embedded.FS

//go:embed greeting.txt
var greeting Text

//go:embed data.bin
var data Data

//go:embed assets
var assets Assets

//go:embed main.mgo
var ownSource string

func Value() string {
	nested, err := assets.ReadFile("assets/nested.txt")
	if err != nil { return err.Error() }
	nested[0] = byte('X')
	nested, err = assets.ReadFile("assets/nested.txt")
	if err != nil || string(nested) != "world" { return "mutable FS" }
	entries, err := assets.ReadDir("assets")
	if err != nil || len(entries) != 1 || entries[0].Name() != "nested.txt" { return "bad directory" }
	if len(ownSource) == 0 { return "source missing" }
	file, err := assets.Open("assets/nested.txt")
	if err != nil { return err.Error() }
	seeker := file.(interface { Seek(int64, int) (int64, error) })
	if _, err = seeker.Seek(1, 0); err != nil { return err.Error() }
	buffer := make([]byte, 2)
	if _, err = file.Read(buffer); err != nil { return err.Error() }
	readerAt := file.(interface { ReadAt([]byte, int64) (int, error) })
	if _, err = readerAt.ReadAt(buffer, 0); err != nil { return err.Error() }
	if _, err = seeker.Seek(-1, 0); err == nil { return "negative seek" }
	pathError, ok := err.(*fs.PathError)
	if !ok || pathError.Path != "assets/nested.txt" || pathError.Op != "seek" { return "bad path error" }
	if len(data) != 2 || data[0] != 0 || data[1] != 1 { return "data mismatch" }
	return string(greeting) + string(nested) + string(buffer)
}
`
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/embed",
		Files:      []source.File{{Path: "main.mgo", Text: mainSource}},
		Resources: []workspace.ResourceFile{
			{Path: "greeting.txt", Data: []byte("hello ")},
			{Path: "data.bin", Data: []byte{0, 1}},
			{Path: "assets/nested.txt", Data: []byte("world")},
			{Path: "main.mgo", Data: []byte(mainSource)},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	value, ok := runTestEntry(t, sources, "example/embed", "value", "Value", minigoruntime.InstanceOptions{}).StringValue()
	if !ok || value != "hello worldwo" {
		t.Fatalf("value = %q, ok=%v", value, ok)
	}
}

func TestEngineRejectsDefinedEmbedFS(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/embed-defined",
		Files: []source.File{{Path: "main.mgo", Text: `package main
import "embed"
type Assets embed.FS
//go:embed value.txt
var assets Assets
`}},
		Resources: []workspace.ResourceFile{{Path: "value.txt", Data: []byte("value")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t, sources)
	result, err := engine.Check("example/embed-defined")
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "semantic.embed.type" {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func TestEngineCompilesDotImportedEmbedFS(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/embed-dot",
		Files: []source.File{{Path: "main.mgo", Text: `package main
import . "embed"
//go:embed value.txt
var files FS
func Value() string {
	data, err := files.ReadFile("value.txt")
	if err != nil { return err.Error() }
	return string(data)
}
`}},
		Resources: []workspace.ResourceFile{{Path: "value.txt", Data: []byte("value")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	value, ok := runTestEntry(t, sources, "example/embed-dot", "value", "Value", minigoruntime.InstanceOptions{}).StringValue()
	if !ok || value != "value" {
		t.Fatalf("value = %q, ok=%v", value, ok)
	}
}
