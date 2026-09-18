package gogen

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/tooling/mrpc"
)

func TestGenerateTypedGoClientProviderAndResource(t *testing.T) {
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "files.mrpc", Text: `syntax = "mrpc/v2";
namespace example.files.v1;
option go_package = "example/files;files";
// Info describes one file.
message Info { name string = 1; size int64 = 2; }
resource File { Read(size int64 = 1) returns (data []uint8 = 1); }
service Files { Open(name string = 1) returns (file File = 1, info Info = 2); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatalf("parse: %#v", diagnostics)
	}
	catalog, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Generate(catalog, Options{})
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		"type FilesHandler interface", "func NewFilesProvider", "func BindFilesClient",
		"type FileHandler interface", "func (c *FileClient) Close", catalog.ResourceTypeID("File"),
		`minigorpc "github.com/d7z-team/mini-go/rpc"`,
		"// Info describes one file.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated Go misses %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "github.com/d7z-team/mini-go/tooling/") {
		t.Fatalf("generated Go imports tooling package:\n%s", text)
	}
}

func TestGenerateRejectsMutatedCatalogIdentity(t *testing.T) {
	file, diagnostics := mrpc.Parse(mrpc.Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
option go_package = "example/service;service";
service Service { Ping() returns (); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	catalog, err := mrpc.NewCatalog([]mrpc.File{file})
	if err != nil {
		t.Fatal(err)
	}
	catalog.ContractID = "mutated"
	if _, err := Generate(catalog, Options{}); err == nil {
		t.Fatal("Generate accepted a catalog with a stale identity")
	}
}

func TestGenerateImportsGoPackageAndUsesContractNamespace(t *testing.T) {
	dependencyFile, diagnostics := mrpc.Parse(mrpc.Source{Path: "model.mrpc", Text: `syntax = "mrpc/v2";
namespace example.model.v1;
option go_package = "example/model;model";
option go_prefix = "API";
message Model { Name string = 1; }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	dependency, err := mrpc.NewCatalog([]mrpc.File{dependencyFile})
	if err != nil {
		t.Fatal(err)
	}
	rootFile, diagnostics := mrpc.Parse(mrpc.Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
option go_package = "example/service;service";
import model "example/model.mrpc";
service Service { Get(request model.Model = 1) returns (); }
`})
	if mrpc.HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	catalog, err := mrpc.NewCatalogWithDependencies([]mrpc.File{rootFile}, map[string]mrpc.Catalog{"example/model.mrpc": dependency})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Generate(catalog, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`model "example/model"`, "Get(context.Context, model.APIModel)", "model.EncodeAPIModel(arg0)", "model.DecodeAPIModel(arguments[0])"} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("generated source misses %q:\n%s", expected, generated)
		}
	}
}
