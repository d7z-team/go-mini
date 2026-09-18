package emit

import (
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler/hir"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestLowerUsesExpressionResultCounts(t *testing.T) {
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.log",
			Name:      "log",
			Signature: testHIRSignature("function() Void"),
		}, {
			ID:        "fn.pair",
			Name:      "pair",
			Signature: testHIRSignature("function() tuple(Int64, Int64)"),
		}, {
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() tuple(Int64, Int64)"),
			Body: []hir.Statement{{
				Kind: hir.StmtExpr,
				Expr: hir.Expression{
					Kind:        hir.ExprCallDirect,
					Function:    "fn.log",
					ResultCount: 0,
				},
			}, {
				Kind: hir.StmtReturn,
				Results: []hir.Expression{{
					Kind:        hir.ExprCallDirect,
					Function:    "fn.pair",
					ResultCount: 2,
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	instructions := artifact.Functions[2].Instructions
	if len(instructions) != 3 {
		t.Fatalf("expected call, call, return with no pop, got %#v", instructions)
	}
	var call ir.CallPayload
	if err := json.Unmarshal(instructions[0].Payload, &call); err != nil {
		t.Fatalf("unmarshal void call payload failed: %v", err)
	}
	if call.ResultCount != 0 {
		t.Fatalf("expected void call result count 0, got %d", call.ResultCount)
	}
	var ret ir.ReturnPayload
	if err := json.Unmarshal(instructions[2].Payload, &ret); err != nil {
		t.Fatalf("unmarshal return payload failed: %v", err)
	}
	if ret.ResultCount != 2 {
		t.Fatalf("expected return result count 2, got %d", ret.ResultCount)
	}
}

func TestLowerStoreResultsUsesReverseStoreOrder(t *testing.T) {
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.pair",
			Name:      "pair",
			Signature: testHIRSignature("function() tuple(Int64, Int64)"),
		}, {
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Void"),
			Locals: []hir.Local{
				{ID: "local.a", Name: "a", Type: testHIRType("Int64")},
				{ID: "local.b", Name: "b", Type: testHIRType("Int64")},
			},
			Body: []hir.Statement{{
				Kind: hir.StmtStoreResults,
				Expr: hir.Expression{
					Kind:        hir.ExprCallDirect,
					Function:    "fn.pair",
					ResultCount: 2,
				},
				Targets: []hir.StoreTarget{
					{Kind: "local", Local: "local.a"},
					{Kind: "local", Local: "local.b"},
				},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	instructions := artifact.Functions[1].Instructions
	if len(instructions) != 3 {
		t.Fatalf("expected call and two stores, got %#v", instructions)
	}
	if instructions[0].Op != string(ir.OpCallDirect) || instructions[1].Op != string(ir.OpStoreLocal) || instructions[2].Op != string(ir.OpStoreLocal) {
		t.Fatalf("expected call_direct/store_local/store_local, got %#v", instructions)
	}
	var firstStore ir.LocalPayload
	if err := json.Unmarshal(instructions[1].Payload, &firstStore); err != nil {
		t.Fatalf("unmarshal first store failed: %v", err)
	}
	var secondStore ir.LocalPayload
	if err := json.Unmarshal(instructions[2].Payload, &secondStore); err != nil {
		t.Fatalf("unmarshal second store failed: %v", err)
	}
	if firstStore.Local != "local.b" || secondStore.Local != "local.a" {
		t.Fatalf("expected reverse store order b,a, got %q,%q", firstStore.Local, secondStore.Local)
	}
}

func TestLowerStoreValuesEvaluatesBeforeReverseStore(t *testing.T) {
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Void"),
			Locals: []hir.Local{
				{ID: "local.a", Name: "a", Type: testHIRType("Int64")},
				{ID: "local.b", Name: "b", Type: testHIRType("Int64")},
			},
			Body: []hir.Statement{{
				Kind: hir.StmtStoreValues,
				Values: []hir.Expression{
					{Kind: hir.ExprLocal, Local: "local.b"},
					{Kind: hir.ExprLocal, Local: "local.a"},
				},
				Targets: []hir.StoreTarget{
					{Kind: "local", Local: "local.a"},
					{Kind: "local", Local: "local.b"},
				},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	instructions := artifact.Functions[0].Instructions
	if len(instructions) != 4 {
		t.Fatalf("expected two loads and two stores, got %#v", instructions)
	}
	if instructions[0].Op != string(ir.OpLoadLocal) || instructions[1].Op != string(ir.OpLoadLocal) ||
		instructions[2].Op != string(ir.OpStoreLocal) || instructions[3].Op != string(ir.OpStoreLocal) {
		t.Fatalf("expected load/load/store/store, got %#v", instructions)
	}
	var firstLoad, secondLoad, firstStore, secondStore ir.LocalPayload
	if err := json.Unmarshal(instructions[0].Payload, &firstLoad); err != nil {
		t.Fatalf("unmarshal first load failed: %v", err)
	}
	if err := json.Unmarshal(instructions[1].Payload, &secondLoad); err != nil {
		t.Fatalf("unmarshal second load failed: %v", err)
	}
	if err := json.Unmarshal(instructions[2].Payload, &firstStore); err != nil {
		t.Fatalf("unmarshal first store failed: %v", err)
	}
	if err := json.Unmarshal(instructions[3].Payload, &secondStore); err != nil {
		t.Fatalf("unmarshal second store failed: %v", err)
	}
	if firstLoad.Local != "local.b" || secondLoad.Local != "local.a" || firstStore.Local != "local.b" || secondStore.Local != "local.a" {
		t.Fatalf("expected load b,a then store b,a, got loads %q,%q stores %q,%q", firstLoad.Local, secondLoad.Local, firstStore.Local, secondStore.Local)
	}
}

func TestLowerStoreLocalPreservesRebindPayload(t *testing.T) {
	literal := hir.Expression{Kind: hir.ExprLiteral, Type: testHIRType("Int64"), Value: json.RawMessage(`1`)}
	artifact, err := lowerTestProgram(t, hir.Program{
		ModulePath: "example/module",
		Package:    "main",
		Functions: []hir.Function{{
			ID:        "fn.main",
			Name:      "main",
			Signature: testHIRSignature("function() Void"),
			Locals:    []hir.Local{{ID: "local.x", Name: "x", Type: testHIRType("Int64")}},
			Body: []hir.Statement{
				{Kind: hir.StmtStoreLocal, Local: "local.x", Expr: literal},
				{Kind: hir.StmtStoreLocal, Local: "local.x", Rebind: true, Expr: literal},
				{Kind: hir.StmtReturn},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	fn := artifact.Functions[0]
	var stores []ir.LocalPayload
	for _, inst := range fn.Instructions {
		if inst.Op != string(ir.OpStoreLocal) {
			continue
		}
		var payload ir.LocalPayload
		if err := json.Unmarshal(inst.Payload, &payload); err != nil {
			t.Fatalf("unmarshal store payload failed: %v", err)
		}
		stores = append(stores, payload)
	}
	if len(stores) != 2 {
		t.Fatalf("expected two store_local instructions, got %#v", fn.Instructions)
	}
	if stores[0].Rebind {
		t.Fatalf("ordinary store_local should not rebind: %#v", stores[0])
	}
	if !stores[1].Rebind {
		t.Fatalf("declaration store_local should preserve rebind: %#v", stores[1])
	}
}
