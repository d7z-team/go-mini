package engine_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/frontend"
)

type staticFrontend struct {
	program *ast.ProgramStmt
}

func (staticFrontend) Language() string {
	return "test/static"
}

func (f staticFrontend) Parse(_ context.Context, files []frontend.SourceFile, _ frontend.Mode) (*ast.ProgramStmt, *frontend.SourceBundle, []error, error) {
	return f.program, frontend.NewSourceBundle(f.Language(), files), nil, nil
}

func TestFrontendCanSupplyMiniASTWithoutRuntimeASTRetention(t *testing.T) {
	program := &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "boot", Meta: "boot", Type: ast.TypeVoid},
		Package:    "custom",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{"Result": &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: ast.TypeInt64}, Value: "0"}},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Main: []ast.Stmt{
			&ast.AssignmentStmt{
				BaseNode: ast.BaseNode{Meta: "assignment", Type: ast.TypeVoid},
				Kind:     ast.AssignSet,
				LHS:      &ast.IdentifierExpr{BaseNode: ast.BaseNode{Meta: "identifier"}, Name: "Result"},
				Value:    &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: ast.TypeInt64}, Value: "42"},
			},
		},
	}

	exec := engine.MustNewMiniExecutor()
	compiled, err := exec.CompileWithFrontend(context.Background(), staticFrontend{program: program}, []engine.SourceFile{{
		URI:      "memory://fixture.static",
		Language: "test/static",
		Code:     "foreign language source",
	}})
	if err != nil {
		t.Fatalf("compile with frontend failed: %v", err)
	}
	if compiled.Program == nil {
		t.Fatal("compiler artifact should retain AST for analysis/compiler boundary")
	}

	runtimeProgram, err := exec.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("load runtime failed: %v", err)
	}
	if runtimeProgram.Compilation().Program != nil {
		t.Fatal("executable program should not retain compiler AST")
	}
	if err := runtimeProgram.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	got, ok := runtimeProgram.SharedState().LoadGlobal("Result")
	if !ok || got == nil || got.I64 != 42 {
		t.Fatalf("unexpected Result global: %#v", got)
	}
}

func TestExecutableProgramTypeHasNoASTFields(t *testing.T) {
	programType := reflect.TypeOf(engine.ExecutableProgram{})
	for _, forbidden := range []string{"Program", "TemplatePreviews", "parentMap"} {
		if _, ok := programType.FieldByName(forbidden); ok {
			t.Fatalf("ExecutableProgram should not expose %s", forbidden)
		}
	}
	for i := 0; i < programType.NumField(); i++ {
		field := programType.Field(i)
		if strings.Contains(field.Type.String(), "/ast.") {
			t.Fatalf("ExecutableProgram field %s retains AST type %s", field.Name, field.Type)
		}
	}
}
