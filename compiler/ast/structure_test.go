package ast

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
)

func TestValidateStructureRejectsCyclesAndResourceOverflow(t *testing.T) {
	cycle := &Expression{Kind: ExprUnary}
	cycle.Operand = cycle
	program := Program{Files: []File{{Decls: []Decl{{
		Kind: DeclFunc,
		Func: FuncDecl{Body: BlockStmt{Stmts: []Statement{{Kind: StmtExpr, Expr: cycle}}}},
	}}}}}
	requireDiagnostic(t, ValidateStructure(&program, Limits{}), "ast.pointer.cycle")

	requireDiagnostic(t, ValidateStructure(&Program{Files: []File{{Path: "main.mgo"}}}, Limits{MaxNodes: 1}), "ast.limit.nodes")

	leaf := &Expression{Kind: ExprIdent, Name: "x"}
	for range 8 {
		leaf = &Expression{Kind: ExprUnary, Operand: leaf}
	}
	program = Program{Files: []File{{Decls: []Decl{{
		Kind: DeclFunc,
		Func: FuncDecl{Body: BlockStmt{Stmts: []Statement{{Kind: StmtExpr, Expr: leaf}}}},
	}}}}}
	requireDiagnostic(t, ValidateStructure(&program, Limits{MaxDepth: 6}), "ast.limit.depth")
}

func TestValidateStructureChecksNodeIdentityAndSpans(t *testing.T) {
	text := "package main\n"
	file := source.NewFile("main", "main.mgo", text)
	fileSpan, _ := file.Span(0, len(text))
	outside, _ := file.Span(0, len(text))
	outside.End.Offset++
	program := Program{
		NodeID: 1,
		Files: []File{{NodeID: 1, Path: file.Path, Span: fileSpan, Decls: []Decl{{
			NodeID: 2, Kind: DeclInvalid, Span: outside,
		}}}},
	}
	diagnostics := ValidateStructure(&program, Limits{})
	requireDiagnostic(t, diagnostics, "ast.node_id.duplicate")
	requireDiagnostic(t, diagnostics, "ast.span.outside_file")
}
