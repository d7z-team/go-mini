package parser

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
)

func findDecl(t *testing.T, decls []ast.Decl, kind ast.DeclKind, name string) ast.Decl {
	t.Helper()
	for _, decl := range decls {
		declaredName := ""
		switch decl.Kind {
		case ast.DeclType:
			declaredName = decl.Type.Name
		case ast.DeclFunc:
			declaredName = decl.Func.Name
		}
		if decl.Kind == kind && declaredName == name {
			return decl
		}
	}
	t.Fatalf("missing %s declaration %q in %+v", kind, name, decls)
	return ast.Decl{}
}

func requireStmtKind(t *testing.T, statements []ast.Statement, kind ast.StmtKind) {
	t.Helper()
	for _, stmt := range statements {
		if stmt.Kind == kind {
			return
		}
	}
	t.Fatalf("missing statement kind %s in %+v", kind, statements)
}

func requireNoDiagnostics(t *testing.T, result Result) {
	t.Helper()
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
}

func requireDiagnostic(t *testing.T, result Result, code string) {
	t.Helper()
	for _, diagnostic := range result.Diagnostics {
		if string(diagnostic.Code) == code {
			return
		}
	}
	t.Fatalf("expected diagnostic %q, got %+v", code, result.Diagnostics)
}
