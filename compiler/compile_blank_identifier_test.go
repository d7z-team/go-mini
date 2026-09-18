package compiler

import (
	"strings"
	"testing"
)

func TestCompileSourceDoesNotBindBlankTopLevelDeclarations(t *testing.T) {
	result, err := compileTestSource("example/blank", "blank.mgo", `
package main

const _ = 1
const _ = 2
var _ = int64(3)
var _ = int64(4)
type _ int64
type _ string

type T int64
func _() {}
func _() {}
func (_ T) _() {}
func (_ T) _() {}

func Main() int64 { return 42 }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}

	for _, constant := range result.Artifact.Constants {
		if strings.Contains(constant.ID, "_") {
			t.Fatalf("blank const leaked into artifact constants: %#v", result.Artifact.Constants)
		}
	}
	for _, global := range result.Artifact.Globals {
		if strings.Contains(global.ID, "_") {
			t.Fatalf("blank global leaked into artifact globals: %#v", result.Artifact.Globals)
		}
	}
	declarations := result.Artifact.TypeTable.DefinedNamed(result.Artifact.Module.Path)
	for _, typ := range declarations {
		if typ.Identity.DeclID == "_" || strings.Contains(string(typ.ID), "_") {
			t.Fatalf("blank type leaked into artifact types: %#v", declarations)
		}
		for _, method := range typ.Methods {
			if method.Name == "_" {
				t.Fatalf("blank method leaked into type metadata: %#v", typ.Methods)
			}
		}
	}
	for _, export := range result.Artifact.Exports {
		if export.Name == "_" || strings.Contains(export.ID, "_") {
			t.Fatalf("blank declaration leaked into exports: %#v", result.Artifact.Exports)
		}
	}
}

func TestCompileSourceRejectsBlankIdentifierReads(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{{
		name: "return blank",
		source: `package main
func Main() int64 { return _ }
`,
		code: "hirgen.ident.unknown",
	}, {
		name: "const blank",
		source: `package main
const _ = 1
const A = _
func Main() int64 { return A }
`,
		code: "hirgen.const.literal",
	}, {
		name: "compound blank",
		source: `package main
func Main() { _ += 1 }
`,
		code: "hirgen.assign.compound.target",
	}, {
		name: "select blank method",
		source: `package main
type T int64
func (_ T) _() {}
	func Main() { T(1)._() }
`,
		code: "hirgen.selector.blank",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/blank", "blank.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}

func TestCompileSourceAllowsBlankSignatureNamesWithoutBindings(t *testing.T) {
	result, err := compileTestSource("example/blank", "blank.mgo", `
package main

type T int64
func (_ T) Value(_ int64) (_ int64) { return }
func Main() int64 { return T(7).Value(99) }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	for _, fn := range result.Artifact.Functions {
		for _, local := range fn.Locals {
			if local.ID == "local._" {
				t.Fatalf("blank signature name was bound as a source local: %#v", fn.Locals)
			}
		}
	}
}

func TestCompileSourceRejectsBlankInterfaceMethod(t *testing.T) {
	result, err := compileTestSource("example/blank", "blank.mgo", `
package main
type I interface { _() }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "ast.type.interface.method.blank")
}
