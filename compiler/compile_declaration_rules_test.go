package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/types"
)

func TestCompileSourcePackageConstForwardReferencesAndCycles(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

const A = B + 1
const B = 41
var V = A

func Main() int64 {
	return int64(V)
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	if got := types.FormatWithTable(&result.Artifact.TypeTable, result.Artifact.Globals[0].Type); got != "Int" {
		t.Fatalf("global V type = %q, want Int", got)
	}

	result, err = compileTestSource("example/scope", "scope.mgo", `
package main

const A = B
const B = A

func Main() int64 {
	return A
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.const.cycle")
}

func TestCompileSourceLocalConstCannotReferenceLaterConst(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{{
		name: "separate declarations",
		source: `
package main

func Main() int64 {
	const A = B
	const B = 1
	return A
}
`,
	}, {
		name: "grouped declarations",
		source: `
package main

func Main() int64 {
	const (
		A = B
		B = 1
	)
	return A
}
`,
	}} {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/scope", "scope.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, "semantic.identifier.unknown")
		})
	}
}

func TestCompileSourceLocalConstForwardCheckAllowsOuterConst(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

const B = 41

func Main() int64 {
	const A = B + 1
	const B = 1
	return A + B - 1
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceRejectsPackageTypeAliasCycles(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{{
		name: "direct",
		source: `
package main

type A = B
type B = A

func Main() int64 {
	return 0
}
`,
	}, {
		name: "nested",
		source: `
package main

type A = []A

func Main() int64 {
	return 0
}
`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/scope", "scope.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, "semantic.type.alias.cycle")
		})
	}
}

func TestCompileSourceAllowsForwardTypeAliasChain(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

type A = B
type B = int64

func Main() int64 {
	var x A = 42
	return x
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceRejectsInvalidDefinedTypeCycles(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{{
		name: "self",
		source: `
package main

type Bad Bad

func Main() int64 {
	return 0
}
`,
	}, {
		name: "array",
		source: `
package main

type Bad [1]Bad

func Main() int64 {
	return 0
}
`,
	}, {
		name: "struct",
		source: `
package main

type Bad struct {
	Next Bad
}

func Main() int64 {
	return 0
}
`,
	}, {
		name: "mutual",
		source: `
package main

type A B
type B struct {
	Value A
}

func Main() int64 {
	return 0
}
`,
	}, {
		name: "through alias",
		source: `
package main

type A B
type B = [1]A

func Main() int64 {
	return 0
}
`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/scope", "scope.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, "semantic.type.defined.cycle")
		})
	}
}

func TestCompileSourceAllowsDefinedTypeCyclesThroughIndirection(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

type Node struct {
	Next *Node
}

type Chain []Chain

type A [1]B
type B []A

func Main() int64 {
	return 0
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceRejectsLocalTypeCycles(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{{
		name: "defined self",
		source: `
package main

func Main() int64 {
	type Bad Bad
	return 0
}
`,
		code: "semantic.type.defined.cycle",
	}, {
		name: "defined array",
		source: `
package main

func Main() int64 {
	type Bad [1]Bad
	return 0
}
`,
		code: "semantic.type.defined.cycle",
	}, {
		name: "defined nested block struct",
		source: `
package main

func Main() int64 {
	{
		type Bad struct {
			Next Bad
		}
	}
	return 0
}
`,
		code: "semantic.type.defined.cycle",
	}, {
		name: "alias self through slice",
		source: `
package main

func Main() int64 {
	type Bad = []Bad
	return 0
}
`,
		code: "semantic.type.alias.cycle",
	}, {
		name: "function literal",
		source: `
package main

func Main() int64 {
	_ = func() {
		type Bad [1]Bad
	}
	return 0
}
`,
		code: "semantic.type.defined.cycle",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/scope", "scope.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}

func TestCompileSourceAllowsLocalTypeCyclesThroughIndirection(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

func Main() int64 {
	type Node struct {
		Next *Node
	}
	type Chain []Chain
	type Lookup map[string]Lookup
	type Stream chan Stream
	type Handler func(Handler)
	_ = Node{}
	return 0
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceRejectsIotaOutsideConstDeclarations(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{{
		name: "package var",
		source: `
package main
var X = iota
func Main() int64 { return X }
`,
	}, {
		name: "function body",
		source: `
package main
func Main() int64 {
	return iota
}
`,
	}} {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/scope", "scope.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, "semantic.identifier.unknown")
		})
	}
}

func TestCompilePackageRejectsFilePackageBlockConflict(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/scope",
		Files: []SourceFile{{
			Path: "a.mgo",
			Text: `package main
import fmt "example/fmt"
func Main() int64 { return 1 }
`,
		}, {
			Path: "b.mgo",
			Text: `package main
var fmt int64
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.scope.file_package")
}
