package mrpc

import (
	"errors"
	"testing"
)

func TestLoadCatalogResolvesTransitiveImports(t *testing.T) {
	sources := map[string]string{
		"model.mrpc": `syntax = "mrpc/v2";
namespace example.model.v1;
option mgo_package = "example/model";
message Model { Name string = 1; }
`,
		"middle.mrpc": `syntax = "mrpc/v2";
namespace example.middle.v1;
option mgo_package = "example/middle";
import model "model.mrpc";
message Middle { Value model.Model = 1; }
`,
	}
	root := Source{Path: "root.mrpc", Text: `syntax = "mrpc/v2";
namespace example.root.v1;
option mgo_package = "example/root";
import middle "middle.mrpc";
service Root { Get(value middle.Middle = 1) returns (); }
`}
	catalog, diagnostics, err := LoadCatalog([]Source{root}, func(path string) (Source, error) {
		text, ok := sources[path]
		if !ok {
			return Source{}, errors.New("missing")
		}
		return Source{Path: path, Text: text}, nil
	})
	if err != nil || HasErrors(diagnostics) {
		t.Fatalf("LoadCatalog = %#v, %#v, %v", catalog, diagnostics, err)
	}
	middle := catalog.Dependencies["middle.mrpc"]
	if middle.Namespace != "example.middle.v1" || middle.Dependencies["model.mrpc"].Namespace != "example.model.v1" {
		t.Fatalf("dependencies = %#v", catalog.Dependencies)
	}
}

func TestLoadCatalogRejectsImportCycle(t *testing.T) {
	root := Source{Path: "root.mrpc", Text: `syntax = "mrpc/v2";
namespace example.root.v1;
import dependency "dependency.mrpc";
service Root { Get(value dependency.Value = 1) returns (); }
`}
	_, _, err := LoadCatalog([]Source{root}, func(path string) (Source, error) {
		return Source{Path: path, Text: `syntax = "mrpc/v2";
namespace example.dependency.v1;
import dependency "dependency.mrpc";
message Value { Name string = 1; }
`}, nil
	})
	if err == nil {
		t.Fatal("import cycle was accepted")
	}
}
