package mrpc

import (
	"fmt"
	"testing"
)

func TestResourceResolutionUsesFileAliasesAndTransitiveOwners(t *testing.T) {
	dependencies := map[string]string{
		"left.mrpc":  `syntax="mrpc/v2"; namespace example.left; import next "leaf.mrpc"; resource File { Next() returns (value next.File = 1); }`,
		"right.mrpc": `syntax="mrpc/v2"; namespace example.right; resource File { Read() returns (value int32 = 1); }`,
		"leaf.mrpc":  `syntax="mrpc/v2"; namespace example.leaf; resource File { Again() returns (value File = 1); }`,
	}
	roots := []Source{
		{Path: "a.mrpc", Text: `syntax="mrpc/v2"; namespace example.root; import model "left.mrpc"; service Left { Open() returns (value model.File = 1); }`},
		{Path: "b.mrpc", Text: `syntax="mrpc/v2"; namespace example.root; import model "right.mrpc"; service Right { Open() returns (value model.File = 1); }`},
	}
	catalog, diagnostics, err := LoadCatalog(roots, func(path string) (Source, error) {
		text, exists := dependencies[path]
		if !exists {
			return Source{}, fmt.Errorf("missing %s", path)
		}
		return Source{Path: path, Text: text}, nil
	})
	if err != nil || HasErrors(diagnostics) {
		t.Fatalf("catalog: %v %+v", err, diagnostics)
	}
	for _, file := range catalog.Files {
		service := file.Services[0]
		resolved, err := catalog.ResolveType(service.Methods[0].Results[0].Type)
		if err != nil {
			t.Fatal(err)
		}
		want := "example.right"
		if service.Name == "Left" {
			want = "example.left"
		}
		if resolved.Owner.Namespace != want {
			t.Fatalf("alias owner = %s, want %s", resolved.Owner.Namespace, want)
		}
		owners := map[string]string{}
		for _, resource := range catalog.ServiceResources(service) {
			owners[resource.Owner.Namespace] = resource.Owner.ResourceTypeID(resource.Name)
		}
		if owners[want] != resolved.Owner.ResourceTypeID("File") {
			t.Fatal("resource contract owner changed")
		}
		if service.Name == "Left" && owners["example.leaf"] == "" {
			t.Fatal("transitive resource method omitted")
		}
		if service.Name == "Right" && owners["example.leaf"] != "" {
			t.Fatal("other file alias polluted closure")
		}
	}
}

func TestEnumWireRangeIsSigned32(t *testing.T) {
	for _, value := range []int64{-2147483649, -2147483648, 2147483647, 2147483648} {
		file, diagnostics := Parse(Source{Path: "enum.mrpc", Text: fmt.Sprintf(`syntax="mrpc/v2"; namespace example.enum; enum State { Value = %d; }`, value)})
		if HasErrors(diagnostics) {
			t.Fatal(diagnostics)
		}
		diagnostics = Validate([]File{file})
		invalid := value < -2147483648 || value > 2147483647
		if HasErrors(diagnostics) != invalid {
			t.Fatalf("value %d: %+v", value, diagnostics)
		}
		if invalid && diagnostics[0].Code != "mrpc.enum.range" {
			t.Fatal(diagnostics)
		}
	}
}
