package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/workspace"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestCompileSourceFoldsMinMaxBuiltins(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

type Score int64

const Low = min(9, 2, 7)
const High = max(9, 2, 7)
const Mixed = min(1, 2.5)

func Main() int64 {
	var scores []Score = []Score{3, 1, 2}
	return int64(min(scores[0], scores[1], scores[2])) +
		int64(max(scores[0], scores[1], scores[2])) + Low + High + int64(Mixed)
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	for _, fn := range result.Artifact.Functions {
		for _, instruction := range fn.Instructions {
			if instruction.Op == string(ir.OpLoadExport) {
				t.Fatalf("constant min/max should be folded before artifact emit: %#v", instruction)
			}
		}
	}
}

func TestCompileSourceRespectsShadowedBuiltins(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

const min = 7

func Main() int64 {
	return int64(min(1, 2))
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected shadowed builtin call to be rejected")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "hirgen.call.type" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected non-callable shadow diagnostic, got %#v", result.Diagnostics)
	}

	result, err = compileTestSource("example/main", "main.mgo", `
package main

import min "example/lib"

func Main() int64 {
	return min(1, 2)
}
`)
	if err != nil {
		t.Fatalf("compileTestSource with import alias failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected import alias to shadow the predeclared builtin")
	}
}

func TestCompileSourceValidatesMinMaxAndNewBuiltins(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{name: "min ellipsis", source: "package main\nfunc Main(values []int64) int64 { return min(values...) }\n", code: "semantic.builtin.minmax.ellipsis"},
		{name: "max ellipsis", source: "package main\nfunc Main(values []int64) int64 { return max(values...) }\n", code: "semantic.builtin.minmax.ellipsis"},
		{name: "new nil", source: "package main\nfunc Main() { _ = new(nil) }\n", code: "hirgen.builtin.new.nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected diagnostic %s", tc.code)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == tc.code {
					return
				}
			}
			t.Fatalf("expected %s, got %#v", tc.code, result.Diagnostics)
		})
	}
}

func TestCompileSourceLowersPrintBuiltinsThroughFmt(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `package main
func print(value int) string { return "shadow" }
func Main() {
	println("value", 42)
	defer println("deferred")
	go println("scheduled")
	_ = print(1)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("compile print builtins: %#v", result.Diagnostics)
	}
	if len(result.Artifact.Requirements) != 1 || result.Artifact.Requirements[0].ModulePath != "fmt" {
		t.Fatalf("print requirements = %#v", result.Artifact.Requirements)
	}
}

func TestCompileSourceRejectsPrintValueAndEllipsis(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		code   string
	}{
		{name: "value", source: "package main\nfunc Main() { value := println(1); _ = value }\n", code: "semantic.assignment.no_value"},
		{name: "ellipsis", source: "package main\nfunc Main(values []any) { println(values...) }\n", code: "semantic.builtin.print.ellipsis"},
		{name: "argument", source: "package main\nfunc Sink(any) {}\nfunc Main() { Sink(println(1)) }\n", code: "semantic.call.argument.no_value"},
		{name: "return", source: "package main\nfunc Main() any { return println(1) }\n", code: "semantic.return.no_value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", test.source)
			if err != nil {
				t.Fatal(err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}

func TestCompileWorkspaceValidatesPrintRuntimeDependency(t *testing.T) {
	for _, test := range []struct {
		name     string
		packages []SourcePackage
		code     string
	}{
		{
			name: "missing fmt",
			packages: []SourcePackage{{ModulePath: "example/main", Files: []SourceFile{{
				Path: "main.mgo", Text: "package main\nfunc main() { println(1) }\n",
			}}}},
			code: "compiler.workspace.module.missing",
		},
		{
			name: "dynamic cycle",
			packages: []SourcePackage{
				{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: "package main\nfunc main() { println(1) }\n"}}},
				{ModulePath: "fmt", Files: []SourceFile{{Path: "builtin.mgo", Text: "package fmt\nimport _ \"example/main\"\nfunc println(args ...any) {}\n"}}},
			},
			code: "compiler.workspace.module.cycle",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources, err := workspace.NewMemorySourceSet(test.packages)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Compile(Request{Root: "example/main", Sources: sources})
			if err != nil {
				t.Fatal(err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}
