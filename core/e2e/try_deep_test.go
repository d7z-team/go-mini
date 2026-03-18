package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestTryCatchDeepPanic(t *testing.T) {
	executor := engine.NewMiniExecutor()
	// 手动构造一个 TryStmt 的 JSON 表达，内部调用一个会 panic 的函数
	tryJson := `
{
  "meta": "boot",
  "variables": {
    "res": { "meta": "literal", "type": "String", "value": "initial" }
  },
  "functions": {
    "doPanic": {
      "meta": "function",
      "name": "doPanic",
      "type": "Void",
      "return": "Void",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "call",
            "func": { "meta": "const_ref", "name": "panic" },
            "args": [ { "meta": "literal", "type": "String", "value": "try-boom" } ]
          }
        ]
      }
    }
  },
  "main": [
    {
      "meta": "try",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "call",
            "func": { "meta": "const_ref", "name": "doPanic" }
          }
        ]
      },
      "catch": {
        "meta": "catch",
        "var_name": "e",
        "body": {
          "meta": "block",
          "children": [
            {
              "meta": "assignment",
              "lhs": { "meta": "identifier", "name": "res" },
              "value": { "meta": "identifier", "name": "e" }
            }
          ]
        }
      }
    },
    {
      "meta": "if",
      "cond": {
        "meta": "binary",
        "operator": "Neq",
        "left": {
          "meta": "call",
          "func": { "meta": "const_ref", "name": "recover" }
        },
        "right": { "meta": "identifier", "name": "nil" }
      },
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "call",
            "func": { "meta": "const_ref", "name": "panic" },
            "args": [ { "meta": "literal", "type": "String", "value": "stale-panic-var-leaked" } ]
          }
        ]
      }
    }
  ]
}
`
	node, err := engine.Unmarshal([]byte(tryJson))
	if err != nil {
		t.Fatal(err)
	}

	program, _, err := engine.ValidateAndOptimize(node, func(v *ast.ValidContext) error {
		v.AddVariable("panic", "function(String) Void")
		v.AddVariable("recover", "function() Any")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := executor.NewRuntimeByAst(program)
	if err != nil {
		t.Fatal(err)
	}

	err = runtime.Execute(context.Background())
	if err != nil {
		if strings.Contains(err.Error(), "stale-panic-var-leaked") {
			t.Fatalf("Test failed: TryStmt leaked PanicVar!")
		}
		t.Fatalf("Expected nil error but got: %v", err)
	}
}
