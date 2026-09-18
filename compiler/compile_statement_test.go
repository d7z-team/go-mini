package compiler

import (
	"testing"
)

func TestCompileSourceValidatesExpressionStatements(t *testing.T) {
	runCompileDiagnosticCases(t, "example/main", []compileDiagnosticCase{
		{name: "plain expression", source: "package main\nfunc Main() { 1 + 2 }\n", code: "semantic.statement.expression"},
		{name: "conversion", source: "package main\ntype Count int64\nfunc Main() { Count(1) }\n", code: "semantic.statement.conversion"},
		{name: "discarded builtin result", source: "package main\nfunc Main() { len(\"value\") }\n", code: "semantic.statement.builtin_value"},
		{name: "multiple expressions", source: "package main\nfunc Main() { 1, 2 }\n", code: "parser.stmt.expr.multi"},
	})
}

func TestCompileSourceValidatesGoAndDeferStatementShape(t *testing.T) {
	runCompileDiagnosticCases(t, "example/main", []compileDiagnosticCase{
		{
			name: "defer non-call",
			source: `
package main
func f() {}
func Main() { defer f }
`,
			code: "parser.defer",
		},
		{
			name: "go non-call",
			source: `
package main
func f() {}
func Main() { go f }
`,
			code: "parser.go",
		},
		{
			name: "defer parenthesized call",
			source: `
package main
func f() {}
func Main() { defer (f()) }
`,
			code: "parser.defer.parenthesized",
		},
		{
			name: "go parenthesized call",
			source: `
package main
func f() {}
func Main() { go (f()) }
`,
			code: "parser.go.parenthesized",
		},
	})
}

func TestCompileSourceAllowsGoAndDeferCallsWithParenthesizedCallee(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main
func f() {}
func Main() {
	defer (f)()
	go (f)()
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceRejectsBlankPackageName(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package _
func Main() {}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected blank package diagnostic")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "parser.package.blank" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected parser.package.blank, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceValidatesInitIdentifierDeclarations(t *testing.T) {
	for _, source := range []string{
		`
package main
const init = 1
func Main() {}
`,
		`
package main
var init int64
func Main() {}
`,
		`
package main
type init int64
func Main() {}
`,
	} {
		result, err := compileTestSource("example/main", "main.mgo", source)
		if err != nil {
			t.Fatalf("compileTestSource failed: %v", err)
		}
		if result.OK() {
			t.Fatalf("expected init declaration diagnostic")
		}
		found := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "hirgen.init.declaration" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected hirgen.init.declaration, got %#v", result.Diagnostics)
		}
	}
}

func TestCompileSourceAllowsInitFunctionAndMethodName(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main
type T int64
func init() {}
func (value T) init() {}
func Main() {}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceDoesNotBindInitFunctionIdentifier(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main
func init() {}
func Main() { init() }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected init call to fail because init does not bind an identifier")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "semantic.identifier.unknown" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected semantic.identifier.unknown, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceValidatesForClausePostStatement(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main
func Main() {
	for i := 0; i < 3; i := i + 1 {
	}
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected for post short declaration diagnostic")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "parser.for.post.short_decl" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected parser.for.post.short_decl, got %#v", result.Diagnostics)
	}

	for _, source := range []string{
		`
package main
func Main() {
	for i := 0; i < 3; i++ {
	}
}
`,
		`
package main
func Main() {
	for i := 0; i < 3; i = i + 1 {
	}
}
`,
	} {
		result, err := compileTestSource("example/main", "main.mgo", source)
		if err != nil {
			t.Fatalf("compileTestSource failed: %v", err)
		}
		if !result.OK() {
			t.Fatalf("expected valid for post statement, got %#v", result.Diagnostics)
		}
	}
}

func TestCompileSourceValidatesReturnResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "void function returns value",
			source: `
package main
func Main() { return 1 }
`,
			code: "hirgen.return.results",
		},
		{
			name: "too few values",
			source: `
package main
func Main() (int64, string) { return 1 }
`,
			code: "hirgen.return.results",
		},
		{
			name: "too many values",
			source: `
package main
func Main() int64 { return 1, 2 }
`,
			code: "hirgen.return.results",
		},
		{
			name: "multi return call count mismatch",
			source: `
package main
func pair() (int64, string) { return 1, "ok" }
func Main() int64 { return pair() }
`,
			code: "hirgen.return.results",
		},
		{
			name: "multi return call type mismatch",
			source: `
package main
func pair() (string, int64) { return "bad", 1 }
func Main() (int64, int64) { return pair() }
`,
			code: "semantic.assign.type",
		},
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

func TestCompileSourceAllowsMultiReturnCall(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main
func pair() (int64, string) { return 1, "ok" }
func Main() (int64, string) { return pair() }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceValidatesBareReturnNamedResultShadowing(t *testing.T) {
	for _, source := range []string{
		`
package main
func Main() (value int64) {
	if true {
		value := int64(1)
		_ = value
		return
	}
	return
}
`,
		`
package main
func Main() (value int64) {
	if true {
		var value int64 = 1
		_ = value
		return
	}
	return
}
`,
	} {
		result, err := compileTestSource("example/main", "main.mgo", source)
		if err != nil {
			t.Fatalf("compileTestSource failed: %v", err)
		}
		if result.OK() {
			t.Fatalf("expected named result shadow diagnostic")
		}
		found := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "hirgen.return.named_shadow" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected hirgen.return.named_shadow, got %#v", result.Diagnostics)
		}
	}
}

func TestCompileSourceAllowsBareReturnWhenNamedResultVisible(t *testing.T) {
	for _, source := range []string{
		`
package main
func Main() (value int64) {
	if true {
		value = 1
		return
	}
	return
}
`,
		`
package main
func Main() (value int64) {
	if true {
		value := int64(1)
		return value
	}
	return
}
`,
	} {
		result, err := compileTestSource("example/main", "main.mgo", source)
		if err != nil {
			t.Fatalf("compileTestSource failed: %v", err)
		}
		if !result.OK() {
			t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
		}
	}
}
