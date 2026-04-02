package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestDeferRecover(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
package main

var res = "initial"

func test() {
	defer func() {
		if r := recover(); r != nil {
			res = "recovered: " + string(r)
		}
	}()
	panic("boom")
}

func main() {
	test()
	if res != "recovered: boom" {
		panic("unexpected res: " + res)
	}
}
`
	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = runtime.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestTryCatchManual(t *testing.T) {
	executor := engine.NewMiniExecutor()
	// 手动构造一个 TryStmt 的 JSON 表达
	// 注意我们需要一个 ProgramStmt 包含 Main
	tryJSON := `
{
  "meta": "boot",
  "variables": {
    "res": { "meta": "literal", "type": "String", "value": "initial" }
  },
  "main": [
    {
      "meta": "try",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "call",
            "func": { "meta": "const_ref", "name": "panic" },
            "args": [ { "meta": "literal", "type": "String", "value": "try-boom" } ]
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
          "meta": "binary", "operator": "Neq",
          "left": { "meta": "identifier", "name": "res" },
          "right": { "meta": "literal", "type": "String", "value": "try-boom" }
       },
       "body": {
          "meta": "block",
          "children": [
             { "meta": "call", "func": { "meta": "const_ref", "name": "panic" }, "args": [ { "meta": "literal", "type": "String", "value": "failed" } ] }
          ]
       }
    }
  ]
}
`
	node, err := engine.Unmarshal([]byte(tryJSON))
	if err != nil {
		t.Fatal(err)
	}

	program, _, err := engine.ValidateAndOptimize(node, func(v *ast.ValidContext) error {
		v.AddVariable("panic", "function(String) Void")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := executor.NewRuntimeByProgram(program)
	if err != nil {
		t.Fatal(err)
	}

	err = runtime.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
