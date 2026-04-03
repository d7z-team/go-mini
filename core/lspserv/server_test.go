package lspserv

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestPackageKeyForURIDistinguishesDirectories(t *testing.T) {
	keyA := packageKeyForURI("file:///workspace/a/main.go", "main")
	keyB := packageKeyForURI("file:///workspace/b/main.go", "main")
	if keyA == keyB {
		t.Fatalf("expected different package keys, got %q", keyA)
	}
}

func TestMergeProgramStmtsIncludesImportsAndTypes(t *testing.T) {
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

	mergeProgramStmts(dest, src)

	if len(dest.Imports) != 2 {
		t.Fatalf("expected merged imports, got %+v", dest.Imports)
	}
	if _, ok := dest.Types["AliasB"]; !ok {
		t.Fatalf("expected merged types, got %+v", dest.Types)
	}
}

func TestLSPServerDoesNotLeakAcrossDirectories(t *testing.T) {
	executor := engine.NewMiniExecutor()
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
