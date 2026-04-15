package lspserv

import (
	"bytes"
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
}

func (s *stubAnalyzer) AnalyzeProgramTolerant(program *ast.ProgramStmt) (ProgramView, []error) {
	return s.program, nil
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
	executor := &stubAnalyzer{program: &stubProgram{}}
	server := NewLSPServer(executor)

	_, _ = server.UpdateSession("file:///workspace/a/main.go", `package main
var sharedA = 1
func FromA() {}
`)
	_, _ = server.UpdateSession("file:///workspace/b/main.go", `package main
func main() {
    
}
`)

	items := server.GetCompletions("file:///workspace/b/main.go", 2, 4)
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
