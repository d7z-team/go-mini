package e2e

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestStructuredDiagnostics(t *testing.T) {
	code := `package main
func main() {
	a := 10
	a = "string" // Type error
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	logs := validator.Logs()
	if len(logs) == 0 {
		t.Fatal("Expected validation errors, got none")
	}

	found := false
	for _, log := range logs {
		if log.Node != nil {
			found = true
			base := log.Node.GetBase()
			if base.Loc == nil {
				t.Errorf("Error log node missing location info")
			} else {
				t.Logf("Error at %d:%d - %d:%d: %s", base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC, log.Message)
				if base.Loc.L != 4 {
					t.Errorf("Expected error at line 4, got line %d", base.Loc.L)
				}
			}
		}
	}

	if !found {
		t.Error("No error logs contained a Node reference")
	}
}
