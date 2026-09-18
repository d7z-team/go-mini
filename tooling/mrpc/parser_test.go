package mrpc

import "testing"

const testContract = `syntax = "mrpc/v2";
namespace example.greeter.v1;
option mgo_package = "example.com/app/greeter";
message Request {
	Name string = 1;
}
message Response {
	Message string = 1;
}
service Greeter {
	Hello(request Request = 1) returns (response Response = 1);
}
`

func TestParseValidateAndContractID(t *testing.T) {
	file, diagnostics := Parse(Source{Path: "greeter.mrpc", Text: testContract})
	if HasErrors(diagnostics) {
		t.Fatalf("parse diagnostics: %#v", diagnostics)
	}
	if diagnostics = Validate([]File{file}); HasErrors(diagnostics) {
		t.Fatalf("validate diagnostics: %#v", diagnostics)
	}
	catalog, err := NewCatalog([]File{file})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Namespace != "example.greeter.v1" || catalog.Options["mgo_package"] != "example.com/app/greeter" || len(catalog.ContractID) != 64 {
		t.Fatalf("catalog = %#v", catalog)
	}
	formatted, diagnostics := Format(Source{Path: "greeter.mrpc", Text: testContract})
	if HasErrors(diagnostics) || formatted == "" {
		t.Fatalf("format = %q, %#v", formatted, diagnostics)
	}
	reparsed, diagnostics := Parse(Source{Path: "greeter.mrpc", Text: formatted})
	reparsedCatalog, err := NewCatalog([]File{reparsed})
	if HasErrors(diagnostics) || err != nil || reparsedCatalog.ContractID != catalog.ContractID {
		t.Fatalf("formatted contract changed identity: %#v", diagnostics)
	}
}

func TestValidateRejectsUnstableFieldIdentity(t *testing.T) {
	file, diagnostics := Parse(Source{Path: "bad.mrpc", Text: `syntax = "mrpc/v2";
namespace example.bad.v1;
message Value { First string = 1; Second string = 1; }
`})
	if HasErrors(diagnostics) {
		t.Fatalf("unexpected parse diagnostics: %#v", diagnostics)
	}
	diagnostics = Validate([]File{file})
	if !HasErrors(diagnostics) || diagnostics[0].Code != "mrpc.field.id_duplicate" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateRejectsHostSizedAndLifecycleTypes(t *testing.T) {
	for _, declaration := range []string{
		"message Value { Number int = 1; }",
		"message Value { Number uint = 1; }",
		"message Value { Address uintptr = 1; }",
		"message Value { Data byte = 1; }",
		"service Service { Call() returns (err error = 1); }",
		"resource Resource { Close() returns (); }",
	} {
		file, diagnostics := Parse(Source{Path: "bad.mrpc", Text: "syntax = \"mrpc/v2\"; namespace example.bad.v1; " + declaration})
		if HasErrors(diagnostics) {
			t.Fatalf("parse %q: %#v", declaration, diagnostics)
		}
		if diagnostics = Validate([]File{file}); !HasErrors(diagnostics) {
			t.Fatalf("validated unstable declaration %q", declaration)
		}
	}
}

func TestValidateRejectsNestedResourceHandles(t *testing.T) {
	file, diagnostics := Parse(Source{Path: "bad.mrpc", Text: `syntax = "mrpc/v2";
namespace example.resource.v1;
resource File { Read() returns (data []uint8 = 1); }
message Holder { File File = 1; }
service Files { Open() returns (file File = 1); }
`})
	if HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	diagnostics = Validate([]File{file})
	if !HasErrors(diagnostics) || diagnostics[0].Code != "mrpc.resource.nested" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateRequiresExplicitImportAlias(t *testing.T) {
	file, diagnostics := Parse(Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
import "example/model.mrpc";
`})
	if HasErrors(diagnostics) {
		t.Fatalf("parse diagnostics: %#v", diagnostics)
	}
	diagnostics = Validate([]File{file})
	if !HasErrors(diagnostics) || diagnostics[0].Code != "mrpc.import.alias" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestCatalogConstructionRequiresValidatedIdentity(t *testing.T) {
	file, diagnostics := Parse(Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
import model "example/model.mrpc";
service Service { Get(request model.Value = 1) returns (); }
`})
	if HasErrors(diagnostics) {
		t.Fatalf("parse diagnostics: %#v", diagnostics)
	}
	if _, err := NewCatalogWithDependencies([]File{file}, nil); err == nil {
		t.Fatal("catalog accepted a missing imported contract")
	}

	dependency, err := NewCatalog([]File{{Path: "model.mrpc", Syntax: Syntax, Namespace: "example.model.v1", Messages: []Message{{Name: "Value"}}}})
	if err != nil {
		t.Fatal(err)
	}
	dependency.ContractID = "invalid"
	if _, err := NewCatalogWithDependencies([]File{file}, map[string]Catalog{"example/model.mrpc": dependency}); err == nil {
		t.Fatal("catalog accepted a dependency with a forged ContractID")
	}
}

func TestContractIDIncludesEnumsAndImportedContracts(t *testing.T) {
	dependencyFile, diagnostics := Parse(Source{Path: "model.mrpc", Text: `syntax = "mrpc/v2";
namespace example.model.v1;
enum State { Unknown = 0; Ready = 1; }
message Model { State State = 1; }
`})
	if HasErrors(diagnostics) {
		t.Fatalf("dependency parse: %#v", diagnostics)
	}
	dependency, err := NewCatalog([]File{dependencyFile})
	if err != nil {
		t.Fatal(err)
	}
	rootFile, diagnostics := Parse(Source{Path: "service.mrpc", Text: `syntax = "mrpc/v2";
namespace example.service.v1;
import model "example/model.mrpc";
service Service { Get(request model.Model = 1) returns (); }
`})
	if HasErrors(diagnostics) {
		t.Fatalf("root parse: %#v", diagnostics)
	}
	root, err := NewCatalogWithDependencies([]File{rootFile}, map[string]Catalog{"example/model.mrpc": dependency})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := root.CanonicalType(rootFile.Services[0].Methods[0].Params[0].Type); err != nil || got != "example.model.v1.Model" {
		t.Fatalf("canonical imported type = %q", got)
	}
	changedDependencyFile := dependencyFile
	changedDependencyFile.Enums = append([]Enum(nil), dependencyFile.Enums...)
	changedDependencyFile.Enums[0].Values = append(changedDependencyFile.Enums[0].Values, EnumValue{Name: "Failed", Number: 2})
	changedDependency, err := NewCatalog([]File{changedDependencyFile})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := NewCatalogWithDependencies([]File{rootFile}, map[string]Catalog{"example/model.mrpc": changedDependency})
	if err != nil {
		t.Fatal(err)
	}
	if root.ContractID == changed.ContractID {
		t.Fatal("imported ContractID did not affect root ContractID")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(testContract)
	f.Fuzz(func(t *testing.T, text string) {
		file, diagnostics := Parse(Source{Path: "fuzz.mrpc", Text: text})
		if !HasErrors(diagnostics) {
			_ = Validate([]File{file})
		}
	})
}

func TestDocCommentsArePreservedButExcludedFromContractIdentity(t *testing.T) {
	withDocs := `syntax = "mrpc/v2";
namespace example.docs.v1;
// Request describes the input.
message Request {
	// Name identifies the caller.
	Name string = 1;
}
// Greeter serves greetings.
service Greeter {
	// Hello returns one greeting.
	Hello(request Request = 1) returns (message string = 1);
}
`
	withoutDocs := `syntax = "mrpc/v2";
namespace example.docs.v1;
message Request { Name string = 1; }
service Greeter { Hello(request Request = 1) returns (message string = 1); }
`
	documented, diagnostics := Parse(Source{Path: "documented.mrpc", Text: withDocs})
	if HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	plain, diagnostics := Parse(Source{Path: "plain.mrpc", Text: withoutDocs})
	if HasErrors(diagnostics) {
		t.Fatal(diagnostics)
	}
	if documented.Messages[0].Doc != "Request describes the input." || documented.Messages[0].Fields[0].Doc != "Name identifies the caller." || documented.Services[0].Methods[0].Doc != "Hello returns one greeting." {
		t.Fatalf("parsed docs = %#v, %#v", documented.Messages[0], documented.Services[0])
	}
	documentedCatalog, err := NewCatalog([]File{documented})
	if err != nil {
		t.Fatal(err)
	}
	plainCatalog, err := NewCatalog([]File{plain})
	if err != nil {
		t.Fatal(err)
	}
	if documentedCatalog.ContractID != plainCatalog.ContractID {
		t.Fatalf("comments changed ContractID: %s != %s", documentedCatalog.ContractID, plainCatalog.ContractID)
	}
}
