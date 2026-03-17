package e2e

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestModuleLoader(t *testing.T) {
	executor := engine.NewMiniExecutor()
	
	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		if path == "mathlib" {
			code := `
			package mathlib
			
			func Add(a int, b int) int {
				return a + b
			}

			func Sub(a int, b int) int {
				return a - b
			}
			`
			converter := ffigo.NewGoToASTConverter()
			node, err := converter.ConvertSource(code)
			if err != nil {
				return nil, err
			}
			return node.(*ast.ProgramStmt), nil
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})

	code := `
	package main
	
	import "mathlib"

	func main() {
		sum := mathlib.Add(10, 20)
		if sum != 30 {
			panic("module mathlib.Add failed")
		}

		sub := mathlib.Sub(50, 20)
		if sub != 30 {
			panic("module mathlib.Sub failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
