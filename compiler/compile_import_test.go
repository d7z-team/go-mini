package compiler

import (
	"testing"

	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/types"
)

func TestSpecializedImportsRetainFileScope(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/lib", Files: []SourceFile{{Path: "lib.mgo", Text: `package lib
type Box[T any] struct { Value T }
func Identity[T any](v T) T { return v }
func Value() int { return 3 }
func Plus[T ~int](v T) int { return Value() + int(v) }
`}}},
		{ModulePath: "example/main", Files: []SourceFile{
			{Path: "a.mgo", Text: "package main\nimport \"example/lib\"\nfunc A() int { return lib.Value() }"},
			{Path: "b.mgo", Text: "package main\nimport \"example/lib\"\nfunc B() lib.Box[int] { return lib.Box[int]{Value: lib.Identity(2)} }"},
			{Path: "c.mgo", Text: "package main\nimport \"example/lib\"\nfunc C() lib.Box[string] { return lib.Box[string]{Value: lib.Identity(\"x\")} }"},
			{Path: "d.mgo", Text: "package main\nimport . \"example/lib\"\nfunc D() int { return Value() }"},
			{Path: "e.mgo", Text: "package main\nimport . \"example/lib\"\nfunc E() Box[int] { return Box[int]{Value: Identity(Plus(2))} }"},
			{Path: "f.mgo", Text: "package main\nimport . \"example/lib\"\nfunc Local[T any](v T) int { return Value() }\nfunc F() int { return Local(1) }"},
		}},
	})
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v, %v", err, result.Diagnostics)
	}
}

func TestSpecializedImportsDistinguishShadowedNames(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/lib", Files: []SourceFile{{Path: "lib.mgo", Text: "package lib\nfunc Value() int { return 1 }"}}},
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/lib"
func Unused[T any](v T) int { return lib.Value() }
func Main() int { lib := struct { Value int }{Value: 2}; return lib.Value }
`}}},
	})
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v, %v", err, result.Diagnostics)
	}
}

func TestGenericResultProjection(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/lib", Files: []SourceFile{{Path: "lib.mgo", Text: `package lib
type Holder[T any] struct { Value T }
func Wrap[T any](v T) []T { return []T{v} }
func Map[T any](v T) map[int]T { return map[int]T{0: v} }
func Equal[T comparable](a, b []T) bool { return len(a) == len(b) }
func Identity[T any](v T) T { return v }
func Pair[T any](v T) (T, int) { return v, 1 }
`}}},
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import . "example/lib"
func Result() bool {
	chunks := Wrap([]int{1, 2})
	var assigned = Wrap([]int{2})
	paired, _ := Pair([]int{3})
	var declared, _ = Pair([]int{4})
	holder := Identity(Holder[[]int]{Value: []int{1}})
	return Equal(chunks[0], []int{1, 2}) && Equal(chunks[0][:1], []int{1}) && Equal(Map([]int{1})[0], []int{1}) && Identity(Wrap("x")[0][0]) == byte('x') && Equal(holder.Value, []int{1}) && Equal(assigned[0], []int{2}) && Equal(paired, []int{3}) && Equal(declared, []int{4})
}
`}}},
	})
	if err != nil || !result.OK() {
		t.Fatalf("compile: %v, %v", err, result.Diagnostics)
	}
}

func TestCompileWorkspaceRejectsUnusedImports(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `package lib
func Value() int64 { return 1 }
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `package main
import lib "example/lib"
func Main() int64 { return 1 }
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.import.unused")
}

func TestCompileWorkspaceRejectsUnusedDotImport(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `package lib
func Value() int64 { return 1 }
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `package main
import . "example/lib"
func Main() int64 { return []struct { Value int64 }{{Value: 1}}[0].Value }
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.import.unused")
}

func TestCompileWorkspaceAllowsBlankImportForSideEffects(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `package lib
func init() {}
func Value() int64 { return 1 }
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `package main
import _ "example/lib"
func Main() int64 { return 1 }
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileWorkspaceRejectsDirectSelfImport(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `package main
import "example/main"
func Main() int64 { return 1 }
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.import.self")
}

func TestCompileWorkspaceRejectsIndirectImportCycle(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/a",
		Files: []SourceFile{{
			Path: "a.mgo",
			Text: `package a
import b "example/b"
func A() int64 { return b.B() }
`,
		}},
	}, {
		ModulePath: "example/b",
		Files: []SourceFile{{
			Path: "b.mgo",
			Text: `package b
import a "example/a"
func B() int64 { return a.A() }
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "compiler.workspace.module.cycle")
}

func TestCompileWorkspaceKeepsDependencyLocalNamedTypesQualifiedPerModule(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/hir",
		Files: []SourceFile{{
			Path: "hir.mgo",
			Text: `package hir
type Upvalue struct { ID string }
type Local struct { ID string }
type Function struct {
	Locals []Local
	Upvalues []Upvalue
}
type Program struct { Functions []Function }
`,
		}},
	}, {
		ModulePath: "example/ir",
		Files: []SourceFile{{
			Path: "ir.mgo",
			Text: `package ir
type Upvalue struct { ID string }
type Local struct { ID string }
type Function struct {
	Locals []Local
	Upvalues []Upvalue
}
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `package main
import h "example/hir"
import i "example/ir"

func consume(functions []h.Function) int64 {
	return int64(len(functions))
}

func consumeLocals(locals []h.Local) int64 {
	return int64(len(locals))
}

func consumeUpvalues(upvalues []h.Upvalue) int64 {
	return int64(len(upvalues))
}

func Main(program h.Program, _ i.Function) int64 {
	return consume(program.Functions) +
		consumeLocals(program.Functions[0].Locals) +
		consumeUpvalues(program.Functions[0].Upvalues)
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	hirArtifact := result.Artifacts["example/hir"]
	for _, decl := range hirArtifact.TypeTable.DefinedNamed(hirArtifact.Module.Path) {
		if decl.Identity.DeclID != "Program" {
			continue
		}
		for _, field := range artifactTypeFields(hirArtifact, decl) {
			if field.Name == "Functions" && types.FormatWithTable(&hirArtifact.TypeTable, field.Type) != "Slice<example/hir.Function>" {
				t.Fatalf("unexpected hir.Program.Functions type: %q", types.FormatWithTable(&hirArtifact.TypeTable, field.Type))
			}
		}
	}
	for _, export := range hirArtifact.Exports {
		if export.Kind != "type" || export.Name != "Function" {
			continue
		}
		info := dependencyExportInfo("example/hir", hirArtifact, export, result.Artifacts)
		for _, field := range info.Fields {
			if field.Name == "Locals" && field.Type != "Slice<example/hir.Local>" {
				t.Fatalf("unexpected hir.Function.Locals dependency type: %q", field.Type)
			}
			if field.Name == "Upvalues" && field.Type != "Slice<example/hir.Upvalue>" {
				t.Fatalf("unexpected hir.Function.Upvalues dependency type: %q", field.Type)
			}
		}
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileWorkspaceQualifiesDependencyLocalTypesInNestedMetadata(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `package lib
type local struct { Value int64 }
type Callback func(local) (local, local)
type Contract interface { Use(local) local }
type Box struct {
	Item local ` + "`json:\"item\"`" + `
	Items []local
	ByName map[string]local
	Callback func(local) (local, local)
	Contract interface { Use(local) local }
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	libArtifact := result.Artifacts["example/lib"]
	exports := map[string]hirgenDependencyExport{}
	for _, export := range libArtifact.Exports {
		if export.Kind == "type" {
			info := dependencyExportInfo("example/lib", libArtifact, export, result.Artifacts)
			exports[export.Name] = hirgenDependencyExport{underlying: info.Underlying, fields: info.Fields}
		}
	}
	if got, want := exports["Callback"].underlying, "function(example/lib.local) tuple(example/lib.local, example/lib.local)"; got != want {
		t.Fatalf("Callback underlying = %q, want %q", got, want)
	}
	if got, want := exports["Contract"].underlying, "interface{Use:function(example/lib.local) example/lib.local}"; got != want {
		t.Fatalf("Contract underlying = %q, want %q", got, want)
	}
	boxFields := map[string]string{}
	for _, field := range exports["Box"].fields {
		boxFields[field.Name] = field.Type
	}
	expectedFields := map[string]string{
		"Item":     "example/lib.local",
		"Items":    "Slice<example/lib.local>",
		"ByName":   "Map<String, example/lib.local>",
		"Callback": "function(example/lib.local) tuple(example/lib.local, example/lib.local)",
		"Contract": "interface{Use:function(example/lib.local) example/lib.local}",
	}
	for name, want := range expectedFields {
		if got := boxFields[name]; got != want {
			t.Fatalf("Box.%s type = %q, want %q", name, got, want)
		}
	}
}

func TestCompileWorkspaceResolvesTypeSelectorsPerFileImportAlias(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/left",
		Files: []SourceFile{{
			Path: "left.mgo",
			Text: `package left
type Box struct { Left int64 }
type Key struct { ID int64 }
type Value struct { ID int64 }
`,
		}},
	}, {
		ModulePath: "example/right",
		Files: []SourceFile{{
			Path: "right.mgo",
			Text: `package right
type Box struct { Right int64 }
type Key struct { ID int64 }
type Value struct { ID int64 }
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "a.mgo",
			Text: `package main
import lib "example/left"

var Left lib.Box
var LeftItems []lib.Value

func UseLeft(v lib.Box, items []lib.Value) map[lib.Key]lib.Value {
	return map[lib.Key]lib.Value{}
}
`,
		}, {
			Path: "b.mgo",
			Text: `package main
import lib "example/right"

var Right lib.Box
var RightItems []lib.Value

func UseRight(v lib.Box, items []lib.Value) map[lib.Key]lib.Value {
	return map[lib.Key]lib.Value{}
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	mainArtifact := result.Artifacts["example/main"]
	mainSymbols := result.PackageSymbols["example/main"]
	globalNames := packageGlobalNames(mainSymbols)
	globalTypes := map[string]string{}
	for _, global := range mainArtifact.Globals {
		globalTypes[globalNames[global.ID]] = types.FormatWithTable(&mainArtifact.TypeTable, global.Type)
	}
	if got, want := globalTypes["Left"], "example/left.Box"; got != want {
		t.Fatalf("Left global type = %q, want %q", got, want)
	}
	if got, want := globalTypes["Right"], "example/right.Box"; got != want {
		t.Fatalf("Right global type = %q, want %q", got, want)
	}
	if got, want := globalTypes["LeftItems"], "Slice<example/left.Value>"; got != want {
		t.Fatalf("LeftItems global type = %q, want %q", got, want)
	}
	if got, want := globalTypes["RightItems"], "Slice<example/right.Value>"; got != want {
		t.Fatalf("RightItems global type = %q, want %q", got, want)
	}
	functionSignatures := map[string]string{}
	for _, symbol := range mainSymbols.Functions {
		if fn, _, ok := artifactFunctionByName(mainArtifact, mainSymbols, symbol.Name); ok {
			functionSignatures[symbol.Name] = types.FormatSignature(&mainArtifact.TypeTable, fn.Signature)
		}
	}
	if got, want := functionSignatures["UseLeft"], "function(example/left.Box, Slice<example/left.Value>) Map<example/left.Key, example/left.Value>"; got != want {
		t.Fatalf("UseLeft signature = %q, want %q", got, want)
	}
	if got, want := functionSignatures["UseRight"], "function(example/right.Box, Slice<example/right.Value>) Map<example/right.Key, example/right.Value>"; got != want {
		t.Fatalf("UseRight signature = %q, want %q", got, want)
	}
}

type hirgenDependencyExport struct {
	underlying string
	fields     []check.DependencyTypeField
}

func TestCompileWorkspaceKeepsDotImportsFileScoped(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `package lib
func Value() int64 { return 1 }
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "a.mgo",
			Text: `package main
import . "example/lib"
func A() int64 { return Value() }
`,
		}, {
			Path: "b.mgo",
			Text: `package main
func Main() int64 { return Value() }
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.identifier.unknown")
}

func TestCompileWorkspaceRejectsDuplicateDotImportNamesInSameFile(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/left",
		Files: []SourceFile{{
			Path: "left.mgo",
			Text: `package left
func Value() int64 { return 1 }
`,
		}},
	}, {
		ModulePath: "example/right",
		Files: []SourceFile{{
			Path: "right.mgo",
			Text: `package right
func Value() int64 { return 2 }
`,
		}},
	}, {
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `package main
import . "example/left"
import . "example/right"
func Main() int64 { return Value() }
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.import.dot.duplicate")
}
