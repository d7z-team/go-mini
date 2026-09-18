package analysis

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/parser"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCheckProgramIndexesExactDefinitionsAndUses(t *testing.T) {
	file := source.NewFile("file:example/main.mgo", "main.mgo", "package main\nfunc f() { x := 1; _ = x }\n")
	parsed := parser.ParseFile("example", file)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("parse diagnostics: %+v", parsed.Diagnostics)
	}
	result := Index(CheckProgram(parsed.Program, check.AnalyzeOptions{}))
	var definitions, references int
	for _, occurrence := range result.Occurrences {
		if occurrence.Name.Text != "x" {
			continue
		}
		if !occurrence.Name.Span.Valid() || occurrence.Object == "" {
			t.Fatalf("incomplete occurrence: %+v", occurrence)
		}
		switch occurrence.Role {
		case ast.NameDefinition:
			definitions++
		case ast.NameReference:
			references++
		}
	}
	if definitions != 1 || references != 1 {
		t.Fatalf("x occurrences = %d definitions, %d references", definitions, references)
	}
}

func TestProjectPublicAPIUsesSemanticTypes(t *testing.T) {
	parsed := parser.ParseSource("example/api", "api.mgo", `package api
const Answer int = 42
var hidden = 1
type Item struct { Name string; hidden int }
type hidden struct{}
type Container struct { hidden }
func Add(left, right int) int { return left + right }
`)
	checked := CheckProgram(parsed.Program, check.AnalyzeOptions{})
	api := ProjectPublicAPI(checked)
	if len(api.Declarations) != 4 {
		t.Fatalf("declarations = %+v", api.Declarations)
	}
	if api.Declarations[0].Name != "Add" || api.Declarations[1].Exact != "42" || api.Declarations[2].Name != "Container" || len(api.Declarations[2].Fields) != 0 || len(api.Declarations[3].Fields) != 1 {
		t.Fatalf("public API = %+v", api.Declarations)
	}
}

func TestProjectPublicAPIIncludesGroupedStringConstantExactValues(t *testing.T) {
	parsed := parser.ParseSource("example/api", "api.mgo", "package api\nconst (\n\tLayout = \"2006-01-02\"\n\tRaw = `line`\n)\n")
	checked := CheckProgram(parsed.Program, check.AnalyzeOptions{})
	api := ProjectPublicAPI(checked)
	if len(api.Declarations) != 2 || api.Declarations[0].Exact != `"2006-01-02"` || api.Declarations[1].Exact != `"line"` {
		t.Fatalf("grouped string constants = %+v", api.Declarations)
	}
}

func TestProjectPublicAPIIncludesConcatenatedStringLiteral(t *testing.T) {
	parsed := parser.ParseSource("example", "value.mgo", "package example\nconst Header = `<prefix>` + \"\\n\"")
	checked := CheckProgram(parsed.Program, check.AnalyzeOptions{})
	api := ProjectPublicAPI(checked)
	if len(api.Declarations) != 1 || api.Declarations[0].Exact != `"<prefix>\n"` {
		t.Fatalf("constant projection: %#v", api.Declarations)
	}
}

func TestProjectPublicAPIResolvesStringConstantReferences(t *testing.T) {
	parsed := parser.ParseSource("example", "api.mgo", "package example\nconst B = A + \"y\"\nconst A = \"x\"\ntype Label string\nconst C Label = B\nconst Letter = string(65)\nconst Size = len(B)\n")
	checked := CheckProgram(parsed.Program, check.AnalyzeOptions{})
	if len(checked.Diagnostics) != 0 {
		t.Fatal(checked.Diagnostics)
	}
	api := ProjectPublicAPI(checked)
	want := map[string]string{"A": `"x"`, "B": `"xy"`, "C": `"xy"`, "Letter": `"A"`, "Size": "2"}
	for _, decl := range api.Declarations {
		if exact, ok := want[decl.Name]; ok {
			if decl.Exact != exact {
				t.Fatalf("%s exact=%q want=%q", decl.Name, decl.Exact, exact)
			}
			delete(want, decl.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing constants: %v", want)
	}
}

func TestProjectPublicAPIIncludesInterfaceMethodsAndTypeParameters(t *testing.T) {
	parsed := parser.ParseSource("example/api", "api.mgo", `package api
type Reader interface { Read([]byte) (int, error) }
type ReadCloser interface { Reader; Close() error }
func Convert[T ~int, U any](value T) U { var zero U; return zero }
`)
	checked := CheckProgram(parsed.Program, check.AnalyzeOptions{})
	if len(checked.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", checked.Diagnostics)
	}
	api := ProjectPublicAPI(checked)
	if len(api.Declarations) != 3 {
		t.Fatalf("declarations = %+v", api.Declarations)
	}
	convert, readCloser := api.Declarations[0], api.Declarations[1]
	if convert.Name != "Convert" || convert.Type != "function(T) U" || len(convert.TypeParams) != 2 || convert.TypeParams[0].Name != "T" || convert.TypeParams[0].Type != "interface{~Int}" || convert.TypeParams[1].Name != "U" || convert.TypeParams[1].Type != "Any" {
		t.Fatalf("generic function = %+v", convert)
	}
	if readCloser.Name != "ReadCloser" || len(readCloser.Methods) != 2 || readCloser.Methods[0].Name != "Close" || readCloser.Methods[1].Name != "Read" {
		t.Fatalf("embedded interface = %+v", readCloser)
	}
}

func TestProjectPublicAPIIncludesPromotedStructMethods(t *testing.T) {
	parsed := parser.ParseSource("example/api", "api.mgo", `package api
type Reader struct{}
func (*Reader) Read([]byte) (int, error) { return 0, nil }
type ReadWriter struct { *Reader }
`)
	checked := CheckProgram(parsed.Program, check.AnalyzeOptions{})
	api := ProjectPublicAPI(checked)
	if len(api.Declarations) != 2 || api.Declarations[0].Name != "ReadWriter" || len(api.Declarations[0].Methods) != 1 || api.Declarations[0].Methods[0].Name != "Read" {
		t.Fatalf("promoted method set = %+v", api.Declarations)
	}
}

func TestCheckWorkspaceInvalidatesPackageWhenResourceChanges(t *testing.T) {
	sources := func(value string) workspace.MemorySourceSet {
		set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
			ModulePath: "example",
			Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
			Resources:  []workspace.ResourceFile{{Path: "value.txt", Data: []byte(value)}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	first, err := CheckWorkspace(WorkspaceRequest{Root: "example", Sources: sources("first")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CheckWorkspace(WorkspaceRequest{Root: "example", Sources: sources("second"), Previous: &first})
	if err != nil {
		t.Fatal(err)
	}
	if second.Stats.PackageCacheHits != 0 || second.Stats.PackageCacheMisses != 1 {
		t.Fatalf("resource change reuse: hits=%d misses=%d", second.Stats.PackageCacheHits, second.Stats.PackageCacheMisses)
	}
}
