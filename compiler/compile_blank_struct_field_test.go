package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/types"
)

func TestCompileSourcePreservesBlankStructFieldMetadata(t *testing.T) {
	result, err := compileTestSource("example/blank-field", "main.mgo", `
package main

type Record struct {
	_ int64 `+"`"+`kind:"number"`+"`"+`
	_ string `+"`"+`kind:"text"`+"`"+`
	Value int64 `+"`"+`json:"value"`+"`"+`
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	record, ok := artifactType(result.Artifact, "Record")
	if !ok {
		t.Fatal("expected Record type metadata")
	}
	fields := artifactTypeFields(result.Artifact, record)
	if len(fields) != 3 {
		t.Fatalf("unexpected Record fields: %#v", fields)
	}
	wantNames := []string{"_", "_", "Value"}
	wantTypes := []string{"Int64", "String", "Int64"}
	wantTags := []string{`kind:"number"`, `kind:"text"`, `json:"value"`}
	for i, field := range fields {
		if field.Name != wantNames[i] || types.FormatWithTable(&result.Artifact.TypeTable, field.Type) != wantTypes[i] || field.Tag != wantTags[i] {
			t.Fatalf("unexpected Record field %d: %#v", i, field)
		}
	}
	view := types.View(&result.Artifact.TypeTable, record.Underlying)
	if !view.Valid() {
		t.Fatal("expected structured Record underlying type")
	}
	viewFields, ok := view.StructFields()
	if !ok || len(viewFields) != 3 || viewFields[0].Name != "_" || viewFields[1].Name != "_" || viewFields[2].Name != "Value" {
		t.Fatalf("unexpected Record underlying fields: %#v", viewFields)
	}
}

func TestCompileSourceChecksBlankStructFieldLiterals(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "keyed blank field",
			source: `package main
type Box struct { _ int64; Value int64 }
func Main() { _ = Box{_: 1} }
`,
			code: "hirgen.composite.struct.unknown",
		},
		{
			name: "blank field type mismatch",
			source: `package main
type Box struct { _ int64; Value int64 }
func Main() { value := "wrong"; _ = Box{value, 1} }
`,
			code: "semantic.assign.type",
		},
		{
			name: "blank field multi result",
			source: `package main
type Box struct { _ int64; Value int64 }
func pair() (int64, int64) { return 1, 2 }
func Main() { _ = Box{pair(), 1} }
`,
			code: "hirgen.composite.struct.value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/blank-field", "main.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}

func TestCompileWorkspaceRejectsImportedUnkeyedBlankStructField(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{
			ModulePath: "example/lib",
			Files: []SourceFile{{Path: "lib.mgo", Text: `package lib
type Box struct { _ int64; Value int64 }
`}},
		},
		{
			ModulePath: "example/main",
			Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/lib"
func Main() { _ = lib.Box{0, 1} }
`}},
		},
	})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "hirgen.composite.struct.unexported")
}
