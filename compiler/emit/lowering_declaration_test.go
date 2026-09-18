package emit

import (
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler/hir"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestLowerProducesDeclaredConstants(t *testing.T) {
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Constants: []hir.Constant{{
			ID:    "const.answer",
			Name:  "answer",
			Type:  testHIRType("Int64"),
			Value: json.RawMessage(`42`),
		}},
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Int64"),
			Body: []hir.Statement{{
				Kind:    hir.StmtReturn,
				Results: []hir.Expression{{Kind: hir.ExprConst, ConstantID: "const.answer"}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if len(artifact.Constants) != 1 || artifact.Constants[0].ID != "const.answer" {
		t.Fatalf("expected declared constant, got %#v", artifact.Constants)
	}
	if got := artifact.Functions[0].Instructions[0].Op; got != string(ir.OpConst) {
		t.Fatalf("expected const instruction, got %q", got)
	}
}

func TestLowerProducesTypeInstructions(t *testing.T) {
	value := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`42`)}
	asAny := hir.Expression{Kind: hir.ExprConvert, Type: testHIRType("Any"), Operand: &value}
	asInt := hir.Expression{Kind: hir.ExprTypeAssert, Type: testHIRType("Int64"), Operand: &asAny}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Int64"),
			Body: []hir.Statement{{
				Kind:    hir.StmtReturn,
				Results: []hir.Expression{asInt},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	instructions := artifact.Functions[0].Instructions
	if got := instructions[1].Op; got != string(ir.OpConvert) {
		t.Fatalf("expected convert instruction, got %q", got)
	}
	if got := instructions[2].Op; got != string(ir.OpTypeAssert) {
		t.Fatalf("expected type_assert instruction, got %q", got)
	}
}

func TestLowerProducesGlobalInstructions(t *testing.T) {
	value := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`42`)}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Globals:    []hir.Global{{ID: "global.answer", Name: "answer", Type: testHIRType("Int64")}},
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Int64"),
			Body: []hir.Statement{{
				Kind:   hir.StmtStoreGlobal,
				Global: "global.answer",
				Expr:   value,
			}, {
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:   hir.ExprGlobal,
					Global: "global.answer",
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if len(artifact.Globals) != 1 || artifact.Globals[0].ID != "global.answer" {
		t.Fatalf("expected lowered global table, got %#v", artifact.Globals)
	}
	instructions := artifact.Functions[0].Instructions
	if got := instructions[1].Op; got != string(ir.OpStoreGlobal) {
		t.Fatalf("expected store_global instruction, got %q", got)
	}
	if got := instructions[2].Op; got != string(ir.OpLoadGlobal) {
		t.Fatalf("expected load_global instruction, got %q", got)
	}
}

func TestLowerProducesUpvalueClosureInstructions(t *testing.T) {
	value := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`41`)}
	one := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)}
	current := hir.Expression{Kind: hir.ExprUpvalue, Upvalue: "up.x"}
	next := hir.Expression{
		Kind:     hir.ExprBinary,
		Operator: "+",
		Left:     &current,
		Right:    &one,
	}
	closure := hir.Expression{
		Kind:     hir.ExprFunction,
		Function: "fn.inc",
		Captures: []hir.CaptureTarget{{Kind: "local", Local: "local.x"}},
	}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.inc",
			Name:      "inc",
			Signature: testHIRSignature("function() Int64"),
			Upvalues:  []hir.Upvalue{{ID: "up.x", Name: "x", Type: testHIRType("Int64")}},
			Body: []hir.Statement{{
				Kind:    hir.StmtStoreUpvalue,
				Upvalue: "up.x",
				Expr:    next,
			}, {
				Kind:    hir.StmtReturn,
				Results: []hir.Expression{{Kind: hir.ExprUpvalue, Upvalue: "up.x"}},
			}},
		}, {
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Function"),
			Locals:    []hir.Local{{ID: "local.x", Name: "x", Type: testHIRType("Int64")}},
			Body: []hir.Statement{{
				Kind:  hir.StmtStoreLocal,
				Local: "local.x",
				Expr:  value,
			}, {
				Kind:    hir.StmtReturn,
				Results: []hir.Expression{closure},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if got := artifact.Functions[0].Instructions[0].Op; got != string(ir.OpLoadUpvalue) {
		t.Fatalf("expected load_upvalue instruction, got %q", got)
	}
	if got := artifact.Functions[0].Instructions[3].Op; got != string(ir.OpStoreUpvalue) {
		t.Fatalf("expected store_upvalue instruction, got %q", got)
	}
	var payload ir.ClosurePayload
	if err := json.Unmarshal(artifact.Functions[1].Instructions[2].Payload, &payload); err != nil {
		t.Fatalf("decode closure payload failed: %v", err)
	}
	if payload.Function != "fn.inc" || len(payload.Captures) != 1 || payload.Captures[0].Local != "local.x" {
		t.Fatalf("unexpected closure payload: %#v", payload)
	}
}
