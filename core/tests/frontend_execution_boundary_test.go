package engine_test

import (
	"context"
	"os"
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
	if compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		t.Fatal("executable artifact should expose prepared bytecode")
	}

	analysis, errs := exec.AnalyzeProgramTolerant(program, nil)
	if len(errs) != 0 {
		t.Fatalf("analysis failed: %v", errs)
	}
	if analysis == nil || analysis.Artifact == nil || analysis.Artifact.Program == nil {
		t.Fatal("analysis artifact should retain AST for analysis boundary")
	}

	runtimeProgram, err := exec.NewRuntimeByArtifact(compiled)
	if err != nil {
		t.Fatalf("load runtime failed: %v", err)
	}
	if _, err := runtimeProgram.Bytecode(); err != nil {
		t.Fatalf("executable program should expose bytecode only: %v", err)
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
	assertStructSourceHasNoASTFields(t, "../mini_program.go", "ExecutableProgram")
	assertStructSourceHasNoASTFields(t, "../artifact.go", "ExecutableArtifact")
}

func assertStructSourceHasNoASTFields(t *testing.T, filename, typeName string) {
	t.Helper()
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	source := string(raw)
	start := strings.Index(source, "type "+typeName+" struct {")
	if start < 0 {
		t.Fatalf("%s missing struct declaration", typeName)
	}
	body := source[start:]
	if lineEnd := strings.IndexByte(body, '\n'); lineEnd >= 0 {
		body = body[lineEnd+1:]
	}
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("%s struct declaration is not closed", typeName)
	}
	body = body[:end]
	fields := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) >= 2 {
			fields[parts[0]] = parts[1]
		}
	}
	for _, forbidden := range []string{"Program", "TemplatePreviews", "parentMap"} {
		if _, ok := fields[forbidden]; ok {
			t.Fatalf("%s should not expose %s", typeName, forbidden)
		}
	}
	for field, typ := range fields {
		if strings.Contains(typ, "*ast.") || strings.Contains(typ, "ast.") || strings.Contains(typ, "/ast.") {
			t.Fatalf("%s field %s retains AST type %s", typeName, field, typ)
		}
	}
}
