package engine_test

import (
	"errors"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/lowering"
	"gopkg.d7z.net/go-mini/core/lspserv"
)

func TestAnalyzeSnapshotDoesNotDuplicateSyntaxAsSemanticDiagnostic(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	uri := "file:///workspace/app/main.mgo"
	result, err := exec.AnalyzeSnapshot(lspserv.PackageSnapshot{
		Key:         "file:///workspace/app::main",
		PackageName: "main",
		RootURI:     "file:///workspace/app",
		Files: []lspserv.SnapshotFile{{
			URI:  uri,
			Code: "package main\nfunc main() {\n",
			Open: true,
		}},
	}, lspserv.AnalysisOptions{CompileParity: true})
	if err != nil {
		t.Fatalf("AnalyzeSnapshot returned server error: %v", err)
	}
	diagnostics := result.Diagnostics[uri]
	if len(diagnostics) == 0 {
		t.Fatal("expected syntax diagnostics")
	}
	var syntaxCount, semanticCount int
	for _, diag := range diagnostics {
		switch diag.Source {
		case "go-mini-syntax":
			syntaxCount++
		case "go-mini-semantic":
			semanticCount++
		}
	}
	if syntaxCount == 0 {
		t.Fatalf("expected syntax diagnostic source, got %+v", diagnostics)
	}
	if semanticCount != 0 {
		t.Fatalf("expected syntax-only parse failure to avoid duplicate semantic diagnostics, got %+v", diagnostics)
	}
}

func TestAnalyzeProgramWithSourcesSkipsLoweringValidation(t *testing.T) {
	c := compiler.New(compiler.Config{})
	_, _, err := c.AnalyzeProgramWithSources("analysis.mgo", "", nestedProgramStmtProgram(), true, nil)
	if err != nil {
		t.Fatalf("analysis front-half should not run lowering: %v", err)
	}
	_, _, err = c.CompileProgramWithSources("analysis.mgo", "", nestedProgramStmtProgram(), true, nil)
	var loweringErr *lowering.Error
	if !errors.As(err, &loweringErr) {
		t.Fatalf("expected full compile to report lowering error, got %v", err)
	}
}

func nestedProgramStmtProgram() *ast.ProgramStmt {
	return &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{Meta: "program"},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{F: "analysis.mgo", L: 1, C: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: ast.TypeVoid,
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{F: "analysis.mgo", L: 1, C: 13}},
					Children: []ast.Stmt{
						&ast.ProgramStmt{
							BaseNode:   ast.BaseNode{Meta: "program", Loc: &ast.Position{F: "analysis.mgo", L: 2, C: 2}},
							Package:    "nested",
							Constants:  map[string]string{},
							Variables:  map[ast.Ident]ast.Expr{},
							Types:      map[ast.Ident]ast.GoMiniType{},
							Structs:    map[ast.Ident]*ast.StructStmt{},
							Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
							Functions:  map[ast.Ident]*ast.FunctionStmt{},
						},
					},
				},
			},
		},
	}
}
