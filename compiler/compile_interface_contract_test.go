package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/types"
)

func TestCompileInterfaceTypeSetContracts(t *testing.T) {
	compiled, err := compileTestSource("example/main", "main.mgo", `
package main

type Numeric interface { int }
type Ordered interface { int | string }
type Reader interface { Read() int64 }
type Wrapped interface { Reader }

func Main() int64 { return 42 }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !compiled.OK() {
		t.Fatalf("expected valid type-set metadata, got %#v", compiled.Diagnostics)
	}
	numeric, ok := artifactType(compiled.Artifact, "Numeric")
	numericType := types.FormatWithTable(&compiled.Artifact.TypeTable, compiled.Artifact.TypeTable.Underlying(types.Ref(numeric)))
	if !ok || numericType != "interface{Int}" {
		t.Fatalf("expected singleton type term in canonical metadata, got %q from %#v", numericType, numeric)
	}
	ordered, ok := artifactType(compiled.Artifact, "Ordered")
	if !ok || types.FormatWithTable(&compiled.Artifact.TypeTable, compiled.Artifact.TypeTable.Underlying(types.Ref(ordered))) != "interface{Int|String}" {
		t.Fatalf("expected union type terms in canonical metadata, got %#v", ordered)
	}
	wrapped, ok := artifactType(compiled.Artifact, "Wrapped")
	wrappedType := types.FormatWithTable(&compiled.Artifact.TypeTable, compiled.Artifact.TypeTable.Underlying(types.Ref(wrapped)))
	if !ok || wrappedType != "interface{example/main.Reader}" {
		t.Fatalf("expected embedded interface metadata, got %q from %#v", wrappedType, wrapped)
	}

	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "duplicate method",
			source: `
package main
type Broken interface { Read() int64; Read() string }
func Main() int64 { return 42 }
`,
			code: "ast.type.interface.method.duplicate",
		},
		{
			name: "embedded method conflict",
			source: `
package main
type Left interface { Read() int64 }
type Right interface { Read() string }
type Broken interface { Left; Right }
func Main() int64 { return 42 }
`,
			code: "semantic.interface.method.conflict",
		},
		{
			name: "embedded cycle",
			source: `
package main
type Left interface { Right }
type Right interface { Left }
func Main() int64 { return 42 }
`,
			code: "semantic.interface.embed.cycle",
		},
		{
			name: "interface term",
			source: `
package main
type Reader interface { Read() int64 }
type Broken interface { Reader | int }
func Main() int64 { return 42 }
`,
			code: "semantic.type_set.term.interface",
		},
		{
			name: "duplicate term",
			source: `
package main
type Broken interface { int | int }
func Main() int64 { return 42 }
`,
			code: "semantic.type_set.term.duplicate",
		},
		{
			name: "overlapping terms",
			source: `
package main
type Broken interface { int | ~int }
func Main() int64 { return 42 }
`,
			code: "semantic.type_set.term.overlap",
		},
		{
			name: "approximation named type",
			source: `
package main
type Score int
type Broken interface { ~Score }
func Main() int64 { return 42 }
`,
			code: "semantic.type_set.term.approx",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected interface diagnostic")
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
