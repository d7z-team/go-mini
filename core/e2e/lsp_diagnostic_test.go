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

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, true)
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

func TestPrimitiveMethodNotFound(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{
			"StringMethodNotFound",
			`package main
			func main() {
				s := "test"
				s.NotFound()
			}`,
		},
		{
			"Int64MethodNotFound",
			`package main
			func main() {
				i := 123
				i.NotFound()
			}`,
		},
		{
			"ArrayMethodNotFound",
			`package main
			func main() {
				a := []Int64{1}
				a.NotFound()
			}`,
		},
		{
			"MapMethodNotFound",
			`package main
			func main() {
				m := map[String]Int64{"a": 1}
				m.NotFound()
			}`,
		},
		{
			"PtrMethodNotFound",
			`package main
			type Point struct { X Int64 }
			func main() {
				p := Point{X: 1}
				p.NotFound()
			}`,
		},
	}

	conv := ffigo.NewGoToASTConverter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := conv.ConvertSource("snippet", tt.code)
			if err != nil {
				t.Fatal(err)
			}
			prog := node.(*ast.ProgramStmt)

			validator, _ := ast.NewValidator(prog, nil, true)
			semanticCtx := ast.NewSemanticContext(validator)
			err = prog.Check(semanticCtx)

			if err == nil {
				t.Errorf("%s: Expected validation error for non-existent method, but got none", tt.name)
			}
		})
	}
}
