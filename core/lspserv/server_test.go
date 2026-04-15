package lspserv

import (
	"bytes"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
)

type stubProgram struct {
	completions []ast.CompletionItem
}

func (p *stubProgram) GetCompletionsAt(line, col int) []ast.CompletionItem { return p.completions }
func (p *stubProgram) GetHoverAt(line, col int) *ast.HoverInfo             { return nil }
func (p *stubProgram) GetDefinitionAt(line, col int) ast.Node              { return nil }
func (p *stubProgram) GetReferencesAt(line, col int) []ast.Node            { return nil }

type stubAnalyzer struct {
	program ProgramView
	errs    []error
}

func (s *stubAnalyzer) AnalyzeProgramTolerant(program *ast.ProgramStmt) (ProgramView, []error) {
	return s.program, s.errs
}

func TestPackageKeyForURIDistinguishesDirectories(t *testing.T) {
	keyA := packageKeyForURI("file:///workspace/a/main.go", "main")
	keyB := packageKeyForURI("file:///workspace/b/main.go", "main")
	if keyA == keyB {
		t.Fatalf("expected different package keys, got %q", keyA)
	}
}

func TestMergeProgramsIncludesImportsAndTypes(t *testing.T) {
	dest := &ast.ProgramStmt{
		Imports:    []ast.ImportSpec{{Path: "fmt"}},
		Types:      map[ast.Ident]ast.GoMiniType{"AliasA": "Int64"},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Variables:  map[ast.Ident]ast.Expr{},
		Constants:  map[string]string{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
	}
	src := &ast.ProgramStmt{
		Imports:    []ast.ImportSpec{{Path: "os"}},
		Types:      map[ast.Ident]ast.GoMiniType{"AliasB": "String"},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Variables:  map[ast.Ident]ast.Expr{},
		Constants:  map[string]string{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
	}

	merged, err := compiler.MergePrograms([]*ast.ProgramStmt{dest, src})
	if err != nil {
		t.Fatalf("MergePrograms failed: %v", err)
	}
	if len(merged.Imports) != 2 {
		t.Fatalf("expected merged imports, got %+v", merged.Imports)
	}
	if _, ok := merged.Types["AliasB"]; !ok {
		t.Fatalf("expected merged types, got %+v", merged.Types)
	}
}

func TestLSPServerDoesNotLeakAcrossDirectories(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	_, _ = server.UpdateSession("file:///workspace/a/main.go", "package main\nvar sharedA = 1\nfunc FromA() {}\n")
	_, _ = server.UpdateSession("file:///workspace/b/main.go", "package main\nfunc main() {}\n")

	items := server.GetCompletions("file:///workspace/b/main.go", 1, 4)
	for _, item := range items {
		if item.Label == "sharedA" || item.Label == "FromA" {
			t.Fatalf("unexpected cross-directory symbol leak: %+v", items)
		}
	}
}

func TestServeStreamInitializeAndCompletion(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{
		program: &stubProgram{
			completions: []ast.CompletionItem{{Label: "Println", Kind: "func", Type: "function(String) Void"}},
		},
	})

	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	openBody := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///workspace/a/main.go","text":"package main\nfunc main() {}\n"}}}`
	completionBody := `{"jsonrpc":"2.0","id":2,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///workspace/a/main.go"},"position":{"line":1,"character":1}}}`

	var in bytes.Buffer
	for _, body := range []string{initBody, openBody, completionBody} {
		in.WriteString("Content-Length: ")
		in.WriteString(strconv.Itoa(len(body)))
		in.WriteString("\r\n\r\n")
		in.WriteString(body)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := ServeStream(server, &in, &out, &errOut); err != nil {
		t.Fatalf("ServeStream returned error: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), `"completionProvider"`) {
		t.Fatalf("expected initialize response, got %q", out.String())
	}
	if !strings.Contains(out.String(), `"label":"Println"`) {
		t.Fatalf("expected completion result, got %q", out.String())
	}
}

func TestUpdateSessionClearsPreviousDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{
		program: &stubProgram{},
		errs: []error{
			&ast.MiniAstError{
				Logs: []ast.Logs{
					{
						Message: "first error",
						Node: &ast.IdentifierExpr{
							BaseNode: ast.BaseNode{Loc: &ast.Position{F: "file:///workspace/a/main.go", L: 1, C: 1, EL: 1, EC: 2}},
						},
					},
				},
			},
		},
	})

	first, _ := server.UpdateSession("file:///workspace/a/main.go", "package main\n")
	if len(first["file:///workspace/a/main.go"]) == 0 {
		t.Fatalf("expected diagnostics on first update, got %+v", first)
	}

	server.executor = &stubAnalyzer{program: &stubProgram{}}
	second, _ := server.UpdateSession("file:///workspace/a/main.go", "package main\n")
	diags, ok := second["file:///workspace/a/main.go"]
	if !ok {
		t.Fatalf("expected clearing diagnostics entry, got %+v", second)
	}
	if len(diags) != 0 {
		t.Fatalf("expected diagnostics to clear, got %+v", diags)
	}
}

func TestUpdateSessionClearsPreviousSyntaxDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	first, _ := server.UpdateSession("file:///workspace/a/main.mgo", "package main\nfunc main() {\n    aaaa\n")
	initial, ok := first["file:///workspace/a/main.mgo"]
	if !ok || len(initial) == 0 {
		t.Fatalf("expected syntax diagnostics on first update, got %+v", first)
	}

	second, _ := server.UpdateSession("file:///workspace/a/main.mgo", "package main\nfunc main() {\n    aaaa\n}\n")
	diags, ok := second["file:///workspace/a/main.mgo"]
	if !ok {
		t.Fatalf("expected clearing diagnostics entry, got %+v", second)
	}
	if len(diags) != 0 {
		t.Fatalf("expected syntax diagnostics to clear, got %+v", diags)
	}
}

func TestUpdateSessionReportsMergeErrorsAsDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	_, _ = server.UpdateSession("file:///workspace/a/main.go", "package main\nfunc helper() {}\n")
	diags, err := server.UpdateSession("file:///workspace/a/other.go", "package main\nfunc helper() {}\n")
	if err == nil {
		t.Fatal("expected merge error")
	}
	found := false
	for _, current := range diags {
		for _, diag := range current {
			if strings.Contains(diag.Message, "duplicate function definition") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected diagnostics for merge error, got %+v", diags)
	}
}

func TestRangeForScannerErrorClampsEOFToVisibleRange(t *testing.T) {
	rng := rangeForScannerError("package main\nfunc main() {\n", scanner.Error{
		Pos: token.Position{Line: 3, Column: 2},
		Msg: "expected '}', found 'EOF'",
	})
	if rng.Start.Line != 1 {
		t.Fatalf("expected EOF to clamp to last real line, got %+v", rng)
	}
	if rng.End.Character <= rng.Start.Character {
		t.Fatalf("expected visible highlight width, got %+v", rng)
	}
}

func TestRangeForScannerErrorUsesTokenWidth(t *testing.T) {
	rng := rangeForScannerError("package main\nvar x = aaaa\n", scanner.Error{
		Pos: token.Position{Line: 2, Column: 9},
		Msg: "expected ';', found aaaa",
	})
	if got := rng.End.Character - rng.Start.Character; got != 4 {
		t.Fatalf("expected token width 4, got %+v", rng)
	}
}
