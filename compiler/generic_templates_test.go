package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/cache"
)

func TestCompileExportsTypedGenericTemplate(t *testing.T) {
	compiled, err := compileTestSource("example/generic", "generic.mgo", `package generic
func Identity[T any](value T) T { return value }
`)
	if err != nil {
		t.Fatalf("compile generic package: %v", err)
	}
	if !compiled.OK() {
		t.Fatalf("compile generic package diagnostics: %#v", compiled.Diagnostics)
	}
	payload, err := cache.EncodeJSON(compiled.ExportData)
	if err != nil {
		t.Fatalf("encode export data: %v", err)
	}
	decoded, err := cache.DecodeJSON(payload)
	if err != nil {
		t.Fatalf("decode export data: %v", err)
	}
	if len(decoded.GenericTemplates) != 1 {
		t.Fatalf("generic template count = %d, want 1", len(decoded.GenericTemplates))
	}
	template := decoded.GenericTemplates[0]
	if template.Name != "Identity" || template.Decl.Kind != "func" || len(template.Decl.Func.TypeParams) != 1 {
		t.Fatalf("unexpected generic template: %#v", template)
	}
}

func TestCompileExportsGenericTemplateUsingImportedGenericType(t *testing.T) {
	compiled, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/iter", Files: []SourceFile{{Path: "iter.mgo", Text: `package iter
type Seq[V any] func(yield func(V) bool)
`}}},
		{ModulePath: "example/maps", Files: []SourceFile{{Path: "maps.mgo", Text: `package maps
import "example/iter"
func Values[V any](values []V) iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, value := range values {
			if !yield(value) { return }
		}
	}
}
`}}},
	})
	if err != nil {
		t.Fatalf("compile generic package: %v", err)
	}
	if !compiled.OK() {
		t.Fatalf("compile generic package diagnostics: %#v", compiled.Diagnostics)
	}
}

func TestCompileImportedGenericUsesTransitiveGenericType(t *testing.T) {
	compiled, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/iter", Files: []SourceFile{{Path: "iter.mgo", Text: `package iter
type Seq[V any] func(yield func(V) bool)
`}}},
		{ModulePath: "example/values", Files: []SourceFile{{Path: "values.mgo", Text: `package values
import "example/iter"
func Values[V any](value V) iter.Seq[V] {
	return func(yield func(V) bool) { yield(value) }
}
`}}},
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/values"
func Main() int {
	result := 0
	values.Values(42)(func(value int) bool { result = value; return true })
	return result
}
`}}},
	})
	if err != nil || !compiled.OK() {
		t.Fatalf("compile transitive generic type: %v, %v", err, compiled.Diagnostics)
	}
}

func TestCompileFunctionUsingImportedGenericType(t *testing.T) {
	compiled, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/iter", Files: []SourceFile{{Path: "iter.mgo", Text: `package iter
type Seq[V any] func(yield func(V) bool)
`}}},
		{ModulePath: "example/values", Files: []SourceFile{{Path: "values.mgo", Text: `package values
import "example/iter"
func Values() iter.Seq[int] {
	return func(yield func(int) bool) { yield(1) }
}
`}}},
	})
	if err != nil {
		t.Fatalf("compile package: %v", err)
	}
	if !compiled.OK() {
		t.Fatalf("compile package diagnostics: %#v", compiled.Diagnostics)
	}
}

func TestCompileImportedGenericCallsExportedPackageHelper(t *testing.T) {
	compiled, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/generic", Files: []SourceFile{{Path: "generic.mgo", Text: `package generic
func Double(value int) int { return value * 2 }
func Apply[T ~int](value T) T { return T(Double(int(value))) }
`}}},
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/generic"
func Main() int { return generic.Apply(21) }
`}}},
	})
	if err != nil {
		t.Fatalf("compile imported generic helper: %v", err)
	}
	if !compiled.OK() {
		t.Fatalf("compile imported generic helper diagnostics: %#v", compiled.Diagnostics)
	}
}

func TestCompileImportedGenericPreservesPackageNamedTypes(t *testing.T) {
	compiled, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/box", Files: []SourceFile{{Path: "box.mgo", Text: `package box
type Box struct { Value int }
func New[T ~int](value T) Box { return Box{Value: int(value)} }
`}}},
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/box"
func Main() int { return box.New(42).Value }
`}}},
	})
	if err != nil {
		t.Fatalf("compile imported generic named type: %v", err)
	}
	if !compiled.OK() {
		t.Fatalf("compile imported generic named type diagnostics: %#v", compiled.Diagnostics)
	}
	artifact, ok := compiled.Artifact("example/main")
	if !ok {
		t.Fatal("expected main artifact")
	}
	for _, requirement := range artifact.Requirements {
		if requirement.ModulePath != "example/box" {
			continue
		}
		for _, export := range requirement.Exports {
			if export == "New" {
				t.Fatalf("specialized generic retained source-level export requirement %q", export)
			}
		}
	}
}

func TestCompileRejectsExpandingGenericInstantiationCycle(t *testing.T) {
	compiled, err := compileTestSource("example/generic-cycle", "generic_cycle.mgo", `package genericcycle
func Expand[T any](value T) { Expand[[]T]([]T{value}) }
func Main() { Expand[int](1) }
`)
	if err != nil {
		t.Fatalf("compile generic cycle: %v", err)
	}
	requireCompileDiagnostic(t, compiled.Diagnostics, "compiler.generic.cycle")
}
