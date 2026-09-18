package compiler

import (
	"testing"
)

func TestCompileSourceRejectsSameBlockRedeclarations(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{{
		name: "parameter var",
		source: `package main
func Main(x int64) int64 {
	var x int64
	return x
}
`,
		code: "hirgen.local.duplicate",
	}, {
		name: "named result const",
		source: `package main
func Main() (out int64) {
	const out = 1
	return
}
`,
		code: "hirgen.local.duplicate",
	}, {
		name: "parameter short no new",
		source: `package main
func Main(x int64) int64 {
	x := int64(1)
	return x
}
`,
		code: "semantic.short.no_new",
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

func TestCompileSourceValidatesShortVariableRedeclarationMatrix(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{{
		name: "ordinary duplicate target",
		source: `package main
func Main() int64 {
	x, x := int64(1), int64(2)
	return x
}
`,
		code: "semantic.short.duplicate",
	}, {
		name: "ordinary blank only is not new",
		source: `package main
func Main() int64 {
	_ := int64(1)
	return 0
}
`,
		code: "semantic.short.no_new",
	}, {
		name: "ordinary reused name plus blank is not new",
		source: `package main
func Main() int64 {
	x := int64(1)
	x, _ := int64(2), int64(3)
	return x
}
`,
		code: "semantic.short.no_new",
	}, {
		name: "ordinary constant redeclare in same block",
		source: `package main
func Main() int64 {
	const x = 1
	x, y := int64(2), int64(3)
	return x + y
}
`,
		code: "semantic.short.variable",
	}, {
		name: "ordinary multi result has no new target",
		source: `package main
func pair() (int64, int64) {
	return 1, 2
}
func Main() int64 {
	x := int64(0)
	y := int64(0)
	x, y := pair()
	return x + y
}
`,
		code: "semantic.short.no_new",
	}, {
		name: "range duplicate target",
		source: `package main
func Main() int64 {
	for i, i := range []int64{1} {
		return i
	}
	return 0
}
`,
		code: "semantic.short.duplicate",
	}, {
		name: "range blank only is not new",
		source: `package main
func Main() int64 {
	for _, _ := range []int64{1} {
		return 1
	}
	return 0
}
`,
		code: "semantic.short.no_new",
	}, {
		name: "select receive blank only is not new",
		source: `package main
func Main() int64 {
	ch := make(chan int64, 1)
	ch <- 1
	select {
	case _, _ := <-ch:
		return 1
	}
	return 0
}
`,
		code: "semantic.short.no_new",
	}, {
		name: "select receive duplicate target",
		source: `package main
func Main() int64 {
	ch := make(chan int64, 1)
	ch <- 1
	select {
	case x, x := <-ch:
		return x
	}
	return 0
}
`,
		code: "semantic.short.duplicate",
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

func TestCompileSourceAllowsNestedBlockShadowing(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

func Main() int64 {
	x := int64(1)
	{
		var x = int64(2)
		if x != 2 {
			return 0
		}
	}
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

func TestCompileSourceUsesOuterScopeInLocalVarInitializer(t *testing.T) {
	result, err := compileTestSource("example/scope", "scope.mgo", `
package main

func Main() int64 {
	x := int64(7)
	{
		var x = x + 1
		return x
	}
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}

	result, err = compileTestSource("example/scope", "scope.mgo", `
package main

func Main() int64 {
	var x = x
	return x
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	requireCompileDiagnostic(t, result.Diagnostics, "semantic.identifier.unknown")
}

func TestCompileSourceRejectsSameConstSpecReferences(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{{
		name: "local const",
		code: "semantic.identifier.unknown",
		source: `
package main

func Main() int64 {
	const A, B = 1, A
	return B
}
`,
	}, {
		name: "package const",
		code: "hirgen.const.same_spec",
		source: `
package main

const A, B = B, 1

func Main() int64 {
	return A
}
`,
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
