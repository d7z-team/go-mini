package engine_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestJSONASTComprehensive(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()

	// 构造一个包含多种复杂节点的 JSON AST
	// 涵盖 switch, slice, map 字面量, 逻辑运算等
	jsonPayload := `
{
  "meta": "boot",
  "variables": {
    "output": { "meta": "literal", "type": "String", "value": "" },
    "s": {
        "meta": "composite",
        "type": "Array<Int64>",
        "values": [
          { "value": { "meta": "literal", "type": "Int64", "value": "10" } },
          { "value": { "meta": "literal", "type": "Int64", "value": "20" } }
        ]
    }
  },
  "main": [
    {
      "meta": "assignment",
      "lhs": { "meta": "identifier", "name": "m" },
      "value": {
        "meta": "composite",
        "type": "Map<String, Int64>",
        "values": [
          { "key": { "meta": "literal", "type": "String", "value": "a" }, "value": { "meta": "literal", "type": "Int64", "value": "1" } }
        ]
      }
    },
    {
      "meta": "switch",
      "tag": { "meta": "literal", "type": "Int64", "value": "1" },
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "case",
            "list": [ { "meta": "literal", "type": "Int64", "value": "1" } ],
            "body": [
              {
                "meta": "assignment",
                "lhs": { "meta": "identifier", "name": "output" },
                "value": { "meta": "literal", "type": "String", "value": "matched" }
              }
            ]
          }
        ]
      }
    },
    {
      "meta": "assignment",
      "lhs": { "meta": "identifier", "name": "slice_val" },
      "value": {
        "meta": "slice",
        "x": { "meta": "identifier", "name": "s" },
        "low": { "meta": "literal", "type": "Int64", "value": "0" },
        "high": { "meta": "literal", "type": "Int64", "value": "1" }
      }
    }
  ]
}
`
	decodedNode, err := engine.Unmarshal([]byte(jsonPayload))
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	validatedProgram, _, err := engine.ValidateAndOptimize(decodedNode, func(v *ast.ValidContext) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	compiled, err := testExecutor.CompileProgram(validatedProgram)
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	testProgram, err := testExecutor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("NewRuntimeByCompiled failed: %v", err)
	}

	err = testProgram.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestCompileProgramSupportsValidatedAST(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	artifact, err := testExecutor.CompileProgram(&ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "test"},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Main:       []ast.Stmt{},
	})
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	if artifact == nil || artifact.Bytecode == nil || artifact.Bytecode.Executable == nil {
		t.Fatal("expected executable artifact")
	}
}
