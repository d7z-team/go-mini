package parser

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/source"
)

func TestParseSourceProgressSafeMalformedBlock(t *testing.T) {
	source := `package main

func Run() {
	for i := 0 i < 3 i++ {
		println(i)
	}
}
`
	result := ParseSource("example/badloop", "badloop.mgo", source)
	if len(result.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics for malformed for clause")
	}
	if len(result.Diagnostics) > 64 {
		t.Fatalf("diagnostic count = %d, parser likely failed to recover: %+v", len(result.Diagnostics), result.Diagnostics)
	}
}

func TestParserReturnsPartialProgramAfterScannerError(t *testing.T) {
	result := ParseSource("example/main", "main.mgo", "package main\nvar bad = \x00\nfunc Good() int { return 1 }\n")
	if len(result.Diagnostics) == 0 || result.Program.Package != "main" {
		t.Fatalf("expected diagnostics and partial package: %#v", result)
	}
	found := false
	for _, decl := range result.Program.Files[0].Decls {
		if decl.Kind == ast.DeclFunc && decl.Func.Name == "Good" {
			found = true
		}
	}
	if !found {
		t.Fatalf("parser did not recover to later declaration: %#v", result.Program.Files[0].Decls)
	}
}

func TestParserLimitsNestingAndDiagnostics(t *testing.T) {
	result := ParseFileWithLimits("example/main", source.NewFile("file.0", "main.mgo", "package main\nfunc F(){((((((((1))))))))}\n"), Limits{
		MaxNesting: 3, MaxDiagnostics: 1,
	})
	if len(result.Diagnostics) == 0 || len(result.Diagnostics) > 2 {
		t.Fatalf("parser limits were not enforced: %#v", result.Diagnostics)
	}
}

func TestParseSourceDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code string
	}{
		{name: "missing package", src: "func main() {}", code: "parser.package"},
		{name: "blank package", src: "package _\nfunc main() {}", code: "parser.package.blank"},
		{name: "missing body", src: "package main\nfunc main()", code: "parser.func.body"},
		{name: "scanner error", src: "package main\nvar s = \"broken\n", code: "scanner.string.newline"},
	}
	for _, tc := range cases {
		result := ParseSource("example/bad", tc.name+".mgo", tc.src)
		requireDiagnostic(t, result, tc.code)
	}
}
