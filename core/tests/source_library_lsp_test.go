package engine_test

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/lspserv"
	"gopkg.d7z.net/go-mini/core/surface"
)

type surfaceLibraryLSPAnalyzer struct {
	exec *engine.MiniExecutor
}

func (a surfaceLibraryLSPAnalyzer) AnalyzeProgramTolerant(program *ast.ProgramStmt, sources map[string]string) (lspserv.ProgramView, []error) {
	return a.exec.AnalyzeProgramTolerant(program, sources)
}

func TestSurfaceLibraryLSPAnalysisRegistersSourceModule(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	if err := exec.UseSurface(lspMathLibrary()); err != nil {
		t.Fatal(err)
	}

	source := `package main

import "mathx"

func main() {
	value := mathx.Double(21)
	_ = value
}
`
	program, errs := exec.AnalyzeGoCodeTolerant(source)
	if program == nil {
		t.Fatalf("expected analysis program, got diagnostics: %v", errs)
	}
	assertNoSourceLibraryDiagnostics(t, errs)

	if program.Artifact == nil || program.Artifact.ImportedPrograms == nil {
		t.Fatal("expected analysis artifact with imported source modules")
	}
	imported := program.Artifact.ImportedPrograms["mathx"]
	if imported == nil {
		t.Fatalf("expected mathx imported program, got %#v", program.Artifact.ImportedPrograms)
	}
	if _, ok := imported.Functions["Double"]; !ok {
		t.Fatalf("expected imported function Double, got %#v", imported.Functions)
	}
	if _, ok := imported.Structs["Counter"]; !ok {
		t.Fatalf("expected imported struct Counter, got %#v", imported.Structs)
	}
	if got := imported.Constants["Version"]; got != "v1" {
		t.Fatalf("expected imported const Version=v1, got %q", got)
	}

	member := findPackageMember(program.Program, "mathx", "Double")
	if member == nil {
		t.Fatal("expected main AST to contain mathx.Double member")
	}
	if got := member.ResolvedPackagePath; got != "mathx" {
		t.Fatalf("expected resolved package path mathx, got %q", got)
	}
	if got := member.ResolvedPackageName; got != "mathx.Double" {
		t.Fatalf("expected resolved package member mathx.Double, got %q", got)
	}
	if got := member.GetBase().Type; got != "function(Int64) Int64" {
		t.Fatalf("expected resolved member type function(Int64) Int64, got %s", got)
	}
	if object := member.Object; object == nil || object.GetBase().Type != ast.TypeModule {
		t.Fatalf("expected mathx object to be TypeModule, got %#v", object)
	}
}

func TestSurfaceLibraryLSPPackageCompletionIncludesSourceMembers(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	if err := exec.UseSurface(lspMathLibrary()); err != nil {
		t.Fatal(err)
	}

	program, _ := exec.AnalyzeGoCodeTolerant(`package main

import "mathx"

func main() {
	mathx.
}
`)
	if program == nil {
		t.Fatal("expected tolerant analysis program")
	}
	items := program.GetCompletionsAt(6, 8)
	assertCompletion(t, items, "Double", "func")
	assertCompletion(t, items, "Counter", "struct")
	assertCompletion(t, items, "Version", "constant")
	assertNoCompletion(t, items, "len")
	assertNoCompletion(t, items, "panic")
	assertNoCompletion(t, items, "Pi")
	assertNoCompletion(t, items, "math.Pi")
}

func TestLSPFFIConstantCompletionDoesNotPolluteProgramConstants(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	program, _ := exec.AnalyzeGoCodeTolerant(`package main

import "math"

func main() {
	math.
}
`)
	if program == nil {
		t.Fatal("expected tolerant analysis program")
	}
	items := program.GetCompletionsAt(6, 7)
	assertCompletion(t, items, "Pi", "constant")
	if _, ok := program.Program.Constants["math.Pi"]; ok {
		t.Fatalf("external FFI constant should not be injected into program constants: %#v", program.Program.Constants)
	}
	if _, ok := program.Program.Constants["Pi"]; ok {
		t.Fatalf("external FFI constant member should not be injected into program constants: %#v", program.Program.Constants)
	}
}

func TestSurfaceLibraryLSPRejectsUnknownSourceMember(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	if err := exec.UseSurface(lspMathLibrary()); err != nil {
		t.Fatal(err)
	}

	_, errs := exec.AnalyzeGoCodeTolerant(`package main

import "mathx"

func main() {
	mathx.len(1)
}
`)
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "package mathx has no member len") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown source member diagnostic, got %v", errs)
	}
}

func TestSurfaceLibraryLSPServerUsesLibraryModuleLoader(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	if err := exec.UseSurface(lspMathLibrary()); err != nil {
		t.Fatal(err)
	}
	server := lspserv.NewLSPServer(surfaceLibraryLSPAnalyzer{exec: exec})
	uri := "file:///workspace/app/main.mgo"
	diagnostics, err := server.UpdateSession(uri, `package main

import "mathx"

func main() {
	mathx.Double(1)
}
`)
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	assertNoLSPDiagnostics(t, diagnostics)
	items := server.GetCompletions(uri, 5, 7)
	assertLSPCompletion(t, items, "Double", lspserv.KindFunction)
	assertLSPCompletion(t, items, "Counter", lspserv.KindStruct)
	assertLSPCompletion(t, items, "Version", lspserv.KindConstant)
}

func findPackageMember(program *ast.ProgramStmt, pkgName, memberName string) *ast.MemberExpr {
	if program == nil {
		return nil
	}
	var found *ast.MemberExpr
	ast.Walk(visitorFunc(func(node ast.Node) bool {
		member, ok := node.(*ast.MemberExpr)
		if !ok || member.Property != ast.Ident(memberName) {
			return true
		}
		if id, ok := member.Object.(*ast.IdentifierExpr); ok && id.Name == ast.Ident(pkgName) {
			found = member
			return false
		}
		return true
	}), program)
	return found
}

func assertNoSourceLibraryDiagnostics(t *testing.T, errs []error) {
	t.Helper()
	for _, err := range errs {
		if strings.Contains(err.Error(), "failed to check package") || strings.Contains(err.Error(), "module not found") {
			t.Fatalf("unexpected source library diagnostic: %v", err)
		}
	}
}

func assertNoLSPDiagnostics(t *testing.T, diagnostics map[string][]lspserv.Diagnostic) {
	t.Helper()
	for uri, current := range diagnostics {
		if len(current) != 0 {
			t.Fatalf("unexpected diagnostics for %s: %+v", uri, current)
		}
	}
}

type visitorFunc func(ast.Node) bool

func (f visitorFunc) Visit(node ast.Node) ast.Visitor {
	if f(node) {
		return f
	}
	return nil
}

func assertCompletion(t *testing.T, items []ast.CompletionItem, label, kind string) {
	t.Helper()
	for _, item := range items {
		if item.Label == label && item.Kind == kind {
			return
		}
	}
	t.Fatalf("missing completion %s/%s in %+v", label, kind, items)
}

func assertNoCompletion(t *testing.T, items []ast.CompletionItem, label string) {
	t.Helper()
	for _, item := range items {
		if item.Label == label {
			t.Fatalf("unexpected completion %s in %+v", label, items)
		}
	}
}

func assertLSPCompletion(t *testing.T, items []lspserv.CompletionItem, label string, kind int) {
	t.Helper()
	for _, item := range items {
		if item.Label == label && item.Kind == kind {
			return
		}
	}
	t.Fatalf("missing completion %s/%d in %+v", label, kind, items)
}

func lspMathLibrary() *surface.Bundle {
	return surface.Library("mathx",
		surface.GoFile("mathx_types.mgo", `
package mathx

const Version = "v1"

type Counter struct {
	Value int
}
`),
		surface.GoFile("mathx_funcs.mgo", `
package mathx

func Double(v int) int {
	return v * 2
}
`),
	)
}
