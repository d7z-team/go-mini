package semantic

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
)

func TestImportedPromotedMethodPreservesReceiverPath(t *testing.T) {
	parsed := parser.ParseSource("example/main", "main.mgo", `package main
import "example/lib"
func Run(value *lib.Outer) int { return value.Read() }
`)
	method := DependencyTypeMethod{
		Name: "Read", Receiver: "Ptr<example/lib.Inner>", Signature: "function() Int",
		FunctionID: "method.Ptr<Inner>.Read", ModulePath: "example/lib",
	}
	checked := WithOptions(parsed.Program, AnalyzeOptions{Dependencies: testDependencies([]DependencyExport{
		{
			ModulePath: "example/lib", Name: "Inner", Kind: ObjectType, Type: "example/lib.Inner",
			Underlying: "struct{Value:Int}", Fields: []DependencyTypeField{{Name: "Value", Type: "Int"}}, Methods: []DependencyTypeMethod{method},
		},
		{
			ModulePath: "example/lib", Name: "Outer", Kind: ObjectType, Type: "example/lib.Outer",
			Underlying: "struct{embedded Inner:Ptr<example/lib.Inner>}",
			Fields:     []DependencyTypeField{{Name: "Inner", Type: "Ptr<example/lib.Inner>", Embedded: true}}, Methods: []DependencyTypeMethod{method},
		},
	})})
	if len(checked.Info.Diagnostics) != 0 {
		t.Fatal(checked.Info.Diagnostics)
	}
	found := false
	for _, selection := range checked.Info.Selections {
		if selection.Kind == SelectionMethod && selection.Name == "Read" {
			found = true
			if len(selection.Index) != 1 || selection.Index[0] != 0 {
				t.Fatalf("path = %v", selection.Index)
			}
		}
	}
	if !found {
		t.Fatal("missing method selection")
	}
}
