package compiler

import "testing"

func TestCompileAllowsShadowedPredeclaredConstructors(t *testing.T) {
	result, err := compileTestSource("example/shadow", "shadow.mgo", `package shadow

func make(value int) int { return value }
func new(value int) int { return value }
func Main() int { return make(1) + new(2) }
`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("shadowed declarations failed to compile: %#v", result.Diagnostics)
	}
}

func TestCompileRejectsInvalidTypeSetTerms(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{name: "duplicate", source: `func F[T int | int](value T) {}`, code: "semantic.type_set.term.duplicate"},
		{name: "overlap", source: `func F[T int | ~int](value T) {}`, code: "semantic.type_set.term.overlap"},
		{name: "interface union term", source: `func F[T interface{ int } | int](value T) {}`, code: "semantic.type_set.term.interface"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/constraint", "constraint.mgo", "package constraint\n"+test.source)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == test.code {
					return
				}
			}
			t.Fatalf("diagnostics = %#v, want %s", result.Diagnostics, test.code)
		})
	}
}

func TestCompileRejectsInvalidStructFieldSets(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "blank field remains part of comparability",
			source: `type Value struct { _ []int }
func Equal(left, right Value) bool { return left == right }`,
			code: "hirgen.binary.comparable",
		},
		{
			name: "duplicate embedded name",
			source: `type Value int
type Container struct { Value; Value }`,
			code: "semantic.struct.field.duplicate",
		},
		{
			name: "named pointer embed",
			source: `type Pointer *int
type Container struct { Pointer }`,
			code: "semantic.struct.embed.invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/structs", "structs.mgo", "package structs\n"+test.source)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == test.code {
					return
				}
			}
			t.Fatalf("diagnostics = %#v, want %s", result.Diagnostics, test.code)
		})
	}
}

func TestCompileResolvesEmbeddedFieldsIndependentOfDeclarationOrder(t *testing.T) {
	result, err := compileTestSource("example/structs", "structs.mgo", `package structs
type Container struct { *Value }
type Value struct { Name string }
`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("forward embedded type failed to compile: %#v", result.Diagnostics)
	}

	result, err = compileTestSource("example/structs", "structs.mgo", `package structs
type Container struct { *Contract }
type Contract interface { Apply() }
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range result.Diagnostics {
		if string(diagnostic.Code) == "semantic.struct.embed.invalid" {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want semantic.struct.embed.invalid", result.Diagnostics)
}

func TestCompileUsesSpecializedTypesForGenericAssignments(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{
			ModulePath: "example/maps",
			Files: []SourceFile{{Path: "maps.mgo", Text: `package maps
func Copy[M1 ~map[K]V, M2 ~map[K]V, K comparable, V any](destination M1, source M2) {
	for key, value := range source {
		destination[key] = value
	}
}
`}},
		},
		{
			ModulePath: "example/model",
			Files:      []SourceFile{{Path: "model.mgo", Text: "package model\ntype Value struct { Name string }\n"}},
		},
		{
			ModulePath: "example/app",
			Files: []SourceFile{
				{Path: "a.mgo", Text: "package app\nfunc Local() {}\n"},
				{Path: "copy.mgo", Text: `package app
import "example/maps"
import model "example/model"
func Copy(destination, source map[string]*model.Value) { maps.Copy(destination, source) }
`},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("specialized assignment failed to compile: %#v", result.Diagnostics)
	}
}
