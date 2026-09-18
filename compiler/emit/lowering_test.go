package emit

import (
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler/hir"
	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestLowerProducesExecutableInstructions(t *testing.T) {
	left := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`2`)}
	right := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`3`)}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:            "fn.main",
			Name:          "main",
			RevisionLocal: true,
			Signature:     testHIRSignature("function() Int64"),
			Body: []hir.Statement{{
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:     hir.ExprBinary,
					Operator: "+",
					Left:     &left,
					Right:    &right,
				}},
			}},
		}},
		Exports: []hir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	if len(artifact.Constants) != 2 {
		t.Fatalf("expected two constants, got %d", len(artifact.Constants))
	}
	if !artifact.Functions[0].RevisionLocal {
		t.Fatal("revision-local function metadata was not emitted")
	}
	if got := artifact.Functions[0].Instructions[2].Op; got != string(ir.OpBinary) {
		t.Fatalf("expected binary instruction, got %q", got)
	}
	if got := artifact.Functions[0].Instructions[3].Op; got != string(ir.OpReturn) {
		t.Fatalf("expected return instruction, got %q", got)
	}
}

func TestLowerLogicalBinaryUsesShortCircuitControlFlow(t *testing.T) {
	left := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Bool"), Value: json.RawMessage(`false`)}
	right := hir.Expression{Kind: hir.ExprCallDirect, Function: "fn.fail", ResultCount: 1}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.fail",
			Name:      "Fail",
			Signature: testHIRSignature("function() Bool"),
			Body: []hir.Statement{{
				Kind:    hir.StmtReturn,
				Results: []hir.Expression{{Kind: hir.ExprLiteral, Type: testHIRType("Bool"), Value: json.RawMessage(`true`)}},
			}},
		}, {
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Bool"),
			Body: []hir.Statement{{
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:     hir.ExprBinary,
					Operator: "&&",
					Left:     &left,
					Right:    &right,
				}},
			}},
		}},
		Exports: []hir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	fn := artifact.Functions[1]
	var sawJumpIf, sawSyntheticLocal, sawLogicalBinary bool
	for _, local := range fn.Locals {
		if local.ID == "__lower.logical.0" && types.FormatWithTable(&artifact.TypeTable, local.Type) == "Bool" {
			sawSyntheticLocal = true
		}
	}
	for _, inst := range fn.Instructions {
		sawJumpIf = sawJumpIf || inst.Op == string(ir.OpJumpIf)
		if inst.Op == string(ir.OpBinary) {
			var payload ir.OperatorPayload
			if err := json.Unmarshal(inst.Payload, &payload); err != nil {
				t.Fatalf("decode operator payload: %v", err)
			}
			sawLogicalBinary = sawLogicalBinary || payload.Operator == "&&"
		}
	}
	if !sawJumpIf || !sawSyntheticLocal || sawLogicalBinary {
		t.Fatalf("expected short-circuit control flow without logical binary, locals=%#v instructions=%#v", fn.Locals, fn.Instructions)
	}
}

func TestLowerBuiltinLenCapExpressions(t *testing.T) {
	array := hir.Expression{
		Kind: hir.ExprSequence,
		Type: testHIRType("Slice<Int64>"),
		Elements: []hir.Expression{
			{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)},
			{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`2`)},
		},
	}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() tuple(Int64, Int64)"),
			Body: []hir.Statement{{
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:    hir.ExprLen,
					Operand: &array,
				}, {
					Kind:    hir.ExprCap,
					Operand: &array,
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	var sawLen, sawCap bool
	for _, inst := range artifact.Functions[0].Instructions {
		sawLen = sawLen || inst.Op == string(ir.OpLen)
		sawCap = sawCap || inst.Op == string(ir.OpCap)
	}
	if !sawLen || !sawCap {
		t.Fatalf("expected len/cap instructions, got %#v", artifact.Functions[0].Instructions)
	}
}

func TestLowerBuiltinAppendDeleteExpressions(t *testing.T) {
	array := hir.Expression{
		Kind: hir.ExprSequence,
		Type: testHIRType("Slice<Int64>"),
		Elements: []hir.Expression{
			{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)},
		},
	}
	key := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)}
	value := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`2`)}
	m := hir.Expression{
		Kind:    hir.ExprMap,
		Type:    testHIRType("Map<Int64, Int64>"),
		Entries: []hir.MapEntry{{Key: key, Value: value}},
	}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Slice<Int64>"),
			Body: []hir.Statement{{
				Kind: hir.StmtExpr,
				Expr: hir.Expression{
					Kind:    hir.ExprDelete,
					Operand: &m,
					Index:   &key,
				},
			}, {
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:    hir.ExprAppend,
					Operand: &array,
					Args: []hir.Expression{
						{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`2`)},
						{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`3`)},
					},
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	var sawAppend, sawDelete, sawPop bool
	for _, inst := range artifact.Functions[0].Instructions {
		sawAppend = sawAppend || inst.Op == string(ir.OpAppend)
		sawDelete = sawDelete || inst.Op == string(ir.OpDelete)
		sawPop = sawPop || inst.Op == string(ir.OpPop)
	}
	if !sawAppend || !sawDelete {
		t.Fatalf("expected append/delete instructions, got %#v", artifact.Functions[0].Instructions)
	}
	if sawPop {
		t.Fatalf("delete expression should produce no pop, got %#v", artifact.Functions[0].Instructions)
	}
}

func TestLowerMapIndexOKExpression(t *testing.T) {
	key := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("String"), Value: json.RawMessage(`"k"`)}
	value := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`42`)}
	m := hir.Expression{
		Kind:    hir.ExprMap,
		Type:    testHIRType("Map<String, Int64>"),
		Entries: []hir.MapEntry{{Key: key, Value: value}},
	}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() tuple(Int64, Bool)"),
			Body: []hir.Statement{{
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:    hir.ExprLoadIndexOK,
					Operand: &m,
					Index:   &key,
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	var sawMapIndexOK bool
	for _, inst := range artifact.Functions[0].Instructions {
		sawMapIndexOK = sawMapIndexOK || inst.Op == string(ir.OpLoadIndexOK)
	}
	if !sawMapIndexOK {
		t.Fatalf("expected load_index_ok instruction, got %#v", artifact.Functions[0].Instructions)
	}
}

func TestLowerBuiltinAppendEllipsisPayload(t *testing.T) {
	array := hir.Expression{
		Kind: hir.ExprSequence,
		Type: testHIRType("Slice<Int64>"),
		Elements: []hir.Expression{
			{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)},
		},
	}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Slice<Int64>"),
			Body: []hir.Statement{{
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:     hir.ExprAppend,
					Operand:  &array,
					Args:     []hir.Expression{array},
					Ellipsis: true,
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	for _, inst := range artifact.Functions[0].Instructions {
		if inst.Op != string(ir.OpAppend) {
			continue
		}
		var payload ir.CountPayload
		if err := json.Unmarshal(inst.Payload, &payload); err != nil {
			t.Fatalf("decode append payload failed: %v", err)
		}
		if payload.Count != 1 || !payload.Expand {
			t.Fatalf("expected expanded append payload, got %#v", payload)
		}
		return
	}
	t.Fatalf("expected append instruction, got %#v", artifact.Functions[0].Instructions)
}

func TestLowerBuiltinClearCopyExpressions(t *testing.T) {
	key := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)}
	value := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`2`)}
	array := hir.Expression{
		Kind: hir.ExprSequence,
		Type: testHIRType("Slice<Int64>"),
		Elements: []hir.Expression{
			{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)},
		},
	}
	m := hir.Expression{
		Kind:    hir.ExprMap,
		Type:    testHIRType("Map<Int64, Int64>"),
		Entries: []hir.MapEntry{{Key: key, Value: value}},
	}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Int64"),
			Body: []hir.Statement{{
				Kind: hir.StmtExpr,
				Expr: hir.Expression{
					Kind:    hir.ExprClear,
					Operand: &m,
				},
			}, {
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:  hir.ExprCopy,
					Left:  &array,
					Right: &array,
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	var sawClear, sawCopy, sawPop bool
	for _, inst := range artifact.Functions[0].Instructions {
		sawClear = sawClear || inst.Op == string(ir.OpClear)
		sawCopy = sawCopy || inst.Op == string(ir.OpCopy)
		sawPop = sawPop || inst.Op == string(ir.OpPop)
	}
	if !sawClear || !sawCopy {
		t.Fatalf("expected clear/copy instructions, got %#v", artifact.Functions[0].Instructions)
	}
	if sawPop {
		t.Fatalf("clear expression should produce no pop, got %#v", artifact.Functions[0].Instructions)
	}
}
