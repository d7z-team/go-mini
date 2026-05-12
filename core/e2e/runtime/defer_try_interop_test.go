package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func compileRuntimeProgram(t *testing.T, src string, validate func(*ast.ValidContext) error) *engine.MiniProgram {
	t.Helper()

	executor := engine.NewMiniExecutor()
	node, err := engine.Unmarshal([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	program, _, err := engine.ValidateAndOptimize(node, validate)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := executor.CompileProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := executor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestFunctionScopedDefersSurviveNestedScopes(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
package main

var trace = ""

func mark(s string) {
	trace = trace + s
}

func demo() {
	{
		defer mark("a")
	}
	for i := 0; i < 2; i++ {
		if i == 0 {
			defer mark("b")
		} else {
			defer mark("c")
		}
	}
	mark("z")
}

func main() {
	demo()
	if trace != "zcba" {
		panic("unexpected trace: " + trace)
	}
}
`
	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTryCatchKeepsFunctionDefer(t *testing.T) {
	tryJSON := `
{
  "meta": "boot",
  "variables": {
    "res": { "meta": "literal", "type": "String", "value": "" }
  },
  "functions": {
    "appendText": {
      "meta": "function",
      "name": "appendText",
      "type": "Void",
      "params": [{ "name": "s", "type": "String" }],
      "return": "Void",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "assignment",
            "kind": "=",
            "lhs": { "meta": "identifier", "name": "res" },
            "value": {
              "meta": "binary",
              "operator": "Plus",
              "left": { "meta": "identifier", "name": "res" },
              "right": { "meta": "identifier", "name": "s" }
            }
          }
        ]
      }
    },
    "demo": {
      "meta": "function",
      "name": "demo",
      "type": "Void",
      "return": "Void",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "defer",
            "call": {
              "meta": "call",
              "func": { "meta": "const_ref", "name": "appendText" },
              "args": [{ "meta": "literal", "type": "String", "value": ":defer" }]
            }
          },
          {
            "meta": "try",
            "body": {
              "meta": "block",
              "children": [
                {
                  "meta": "call",
                  "func": { "meta": "const_ref", "name": "panic" },
                  "args": [{ "meta": "literal", "type": "String", "value": "boom" }]
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
                    "meta": "call",
                    "func": { "meta": "const_ref", "name": "appendText" },
                    "args": [
                      {
                        "meta": "binary",
                        "operator": "Plus",
                        "left": { "meta": "literal", "type": "String", "value": ":catch=" },
                        "right": { "meta": "identifier", "name": "e" }
                      }
                    ]
                  }
                ]
              }
            }
          }
        ]
      }
    }
  },
  "main": [
    {
      "meta": "call",
      "func": { "meta": "const_ref", "name": "demo" }
    },
    {
      "meta": "if",
      "cond": {
        "meta": "binary",
        "operator": "Neq",
        "left": { "meta": "identifier", "name": "res" },
        "right": { "meta": "literal", "type": "String", "value": ":catch=boom:defer" }
      },
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "call",
            "func": { "meta": "const_ref", "name": "panic" },
            "args": [
              {
                "meta": "binary",
                "operator": "Plus",
                "left": { "meta": "literal", "type": "String", "value": "unexpected res: " },
                "right": { "meta": "identifier", "name": "res" }
              }
            ]
          }
        ]
      }
    }
  ]
}
`

	runtime := compileRuntimeProgram(t, tryJSON, func(v *ast.ValidContext) error {
		v.AddVariable("panic", "function(String) Void")
		return nil
	})
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTryFinallyRunsBeforeRecoveringFunctionDefer(t *testing.T) {
	tryJSON := `
{
  "meta": "boot",
  "variables": {
    "res": { "meta": "literal", "type": "String", "value": "" }
  },
  "functions": {
    "appendText": {
      "meta": "function",
      "name": "appendText",
      "type": "Void",
      "params": [{ "name": "s", "type": "String" }],
      "return": "Void",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "assignment",
            "kind": "=",
            "lhs": { "meta": "identifier", "name": "res" },
            "value": {
              "meta": "binary",
              "operator": "Plus",
              "left": { "meta": "identifier", "name": "res" },
              "right": { "meta": "identifier", "name": "s" }
            }
          }
        ]
      }
    },
    "captureRecover": {
      "meta": "function",
      "name": "captureRecover",
      "type": "Void",
      "return": "Void",
      "body": {
        "meta": "block",
        "children": [
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
                  "func": { "meta": "const_ref", "name": "appendText" },
                  "args": [{ "meta": "literal", "type": "String", "value": ":recovered" }]
                }
              ]
            }
          }
        ]
      }
    },
    "demo": {
      "meta": "function",
      "name": "demo",
      "type": "Void",
      "return": "Void",
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "defer",
            "call": {
              "meta": "call",
              "func": { "meta": "const_ref", "name": "captureRecover" }
            }
          },
          {
            "meta": "try",
            "body": {
              "meta": "block",
              "children": [
                {
                  "meta": "call",
                  "func": { "meta": "const_ref", "name": "panic" },
                  "args": [{ "meta": "literal", "type": "String", "value": "boom" }]
                }
              ]
            },
            "finally": {
              "meta": "block",
              "children": [
                {
                  "meta": "call",
                  "func": { "meta": "const_ref", "name": "appendText" },
                  "args": [{ "meta": "literal", "type": "String", "value": ":finally" }]
                }
              ]
            }
          }
        ]
      }
    }
  },
  "main": [
    {
      "meta": "call",
      "func": { "meta": "const_ref", "name": "demo" }
    },
    {
      "meta": "if",
      "cond": {
        "meta": "binary",
        "operator": "Neq",
        "left": { "meta": "identifier", "name": "res" },
        "right": { "meta": "literal", "type": "String", "value": ":finally:recovered" }
      },
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "call",
            "func": { "meta": "const_ref", "name": "panic" },
            "args": [
              {
                "meta": "binary",
                "operator": "Plus",
                "left": { "meta": "literal", "type": "String", "value": "unexpected res: " },
                "right": { "meta": "identifier", "name": "res" }
              }
            ]
          }
        ]
      }
    }
  ]
}
`

	runtime := compileRuntimeProgram(t, tryJSON, func(v *ast.ValidContext) error {
		v.AddVariable("panic", "function(String) Void")
		v.AddVariable("recover", "function() Any")
		return nil
	})
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
