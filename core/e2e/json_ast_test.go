package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestJSONASTComprehensive(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 构造一个包含多种复杂节点的 JSON AST
	// 涵盖 switch, slice, map 字面量, 逻辑运算等
	jsonAST := `
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
	node, err := engine.Unmarshal([]byte(jsonAST))
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	program, _, err := engine.ValidateAndOptimize(node, func(v *ast.ValidContext) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	runtime, err := executor.NewRuntimeByAst(program)
	if err != nil {
		t.Fatalf("NewRuntimeByAst failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
