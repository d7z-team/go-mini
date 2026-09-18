package semantic

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
)

func TestImportResolutionDistinguishesEmptyAndUnavailablePackages(t *testing.T) {
	parsed := parser.ParseSource("example/main", "main.mgo", "package main\nimport renamed \"example/lib\"\nfunc Main() { renamed.Missing() }\n")
	for _, tc := range []struct {
		name    string
		options AnalyzeOptions
		code    string
	}{
		{"unavailable", AnalyzeOptions{}, "semantic.dependency.unavailable"},
		{"empty", AnalyzeOptions{Dependencies: []DependencyPackage{{ModulePath: "example/lib"}}}, "semantic.import.member.missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checked := WithOptions(parsed.Program, tc.options)
			if len(checked.Info.Diagnostics) != 1 || string(checked.Info.Diagnostics[0].Code) != tc.code {
				t.Fatalf("diagnostics: %v", checked.Info.Diagnostics)
			}
			if tc.name == "empty" && !strings.Contains(checked.Info.Diagnostics[0].Message, "renamed.Missing") {
				t.Fatal("diagnostic lost source import alias")
			}
		})
	}
}

func TestImportResolutionRespectsLocalShadowing(t *testing.T) {
	parsed := parser.ParseSource("example/main", "main.mgo", `package main
import lib "example/lib"
var value lib.Value
func Main() int { lib := struct { Count int }{Count: 42}; return lib.Count }
`)
	checked := WithOptions(parsed.Program, AnalyzeOptions{Dependencies: []DependencyPackage{{ModulePath: "example/lib", Members: []DependencyExport{{Name: "Value", Kind: ObjectType, Type: "example/lib.Value", Underlying: "Int"}}}}})
	if len(checked.Info.Diagnostics) != 0 {
		t.Fatal(checked.Info.Diagnostics)
	}
}

func TestDotImportMissingValueIsDiagnosedDuringCheck(t *testing.T) {
	parsed := parser.ParseSource("example/main", "main.mgo", "package main\nimport . \"example/lib\"\nfunc Main() { _ = Present; _ = Missing }\n")
	checked := WithOptions(parsed.Program, AnalyzeOptions{Dependencies: []DependencyPackage{{ModulePath: "example/lib", Members: []DependencyExport{{Name: "Present", Kind: ObjectVar, Type: "Int"}}}}})
	if len(checked.Info.Diagnostics) != 1 || checked.Info.Diagnostics[0].Code != "semantic.identifier.unknown" {
		t.Fatalf("diagnostics: %v", checked.Info.Diagnostics)
	}
}

func TestDotImportMissingTypeIsDiagnosedDuringCheck(t *testing.T) {
	parsed := parser.ParseSource("example/main", "main.mgo", "package main\nimport . \"example/lib\"\nvar value Missing\nfunc Main() { _ = Present }\n")
	checked := WithOptions(parsed.Program, AnalyzeOptions{Dependencies: []DependencyPackage{{ModulePath: "example/lib", Members: []DependencyExport{{Name: "Present", Kind: ObjectVar, Type: "Int"}}}}})
	if len(checked.Info.Diagnostics) != 1 || checked.Info.Diagnostics[0].Code != "semantic.type.unknown" {
		t.Fatalf("diagnostics: %v", checked.Info.Diagnostics)
	}
}

func TestUndefinedValueReferencesAreDiagnosedDuringCheck(t *testing.T) {
	for _, body := range []string{"func Main() { Missing() }", "func Main() int { return Missing }", "var Value = Missing"} {
		parsed := parser.ParseSource("example/main", "main.mgo", "package main\n"+body+"\n")
		checked := Check(parsed.Program)
		if len(checked.Info.Diagnostics) != 1 || checked.Info.Diagnostics[0].Code != "semantic.identifier.unknown" {
			t.Fatalf("%s: %v", body, checked.Info.Diagnostics)
		}
	}
}

func TestGenericReceiverTypeParametersAreAvailableInMethodSignatures(t *testing.T) {
	parsed := parser.ParseSource("example/main", "main.mgo", `package main
type Pair[T any] struct { Value T }
func (p *Pair[T]) Get() T { return p.Value }
`)
	checked := Check(parsed.Program)
	if len(checked.Info.Diagnostics) != 0 {
		t.Fatal(checked.Info.Diagnostics)
	}
}
