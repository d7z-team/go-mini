package runtime

import (
	"encoding/json"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestVMRunsDirectCallWithLocals(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.input", Type: testType("String"), Value: json.RawMessage(`"ok"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.echo",
		Signature: testSignature("function(String) String"),
		Locals:    []ir.Local{{ID: "local.value", Type: testType("String")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.value"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}, {
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.input"}`),
		}, {
			Op:      string(ir.OpCallDirect),
			Payload: json.RawMessage(`{"function":"fn.echo","arg_count":1,"result_count":1}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	result, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("run export failed: %v", err)
	}
	requireValues(t, result.Values, newVMValue("String", "ok"))
}

func TestVMTailCallDoesNotConsumeCallDepth(t *testing.T) {
	artifact := ir.NewArtifact("example/tail", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.depth", Type: testType("Int64"), Value: json.RawMessage(`5000`)},
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
	}
	artifact.Functions = []ir.Function{
		{
			ID: "fn.count", Signature: testSignature("function(Int64) Int64"),
			Locals: []ir.Local{{ID: "local.n", Type: testType("Int64")}},
			Instructions: []ir.Instruction{
				{Op: string(ir.OpLoadLocal), Payload: testPayload(ir.LocalPayload{Local: "local.n"})},
				{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "c.zero"})},
				{Op: string(ir.OpBinary), Payload: testPayload(ir.OperatorPayload{Operator: "=="})},
				{Op: string(ir.OpJumpIf), Payload: testPayload(ir.JumpPayload{Label: "done"})},
				{Op: string(ir.OpLoadLocal), Payload: testPayload(ir.LocalPayload{Local: "local.n"})},
				{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "c.one"})},
				{Op: string(ir.OpBinary), Payload: testPayload(ir.OperatorPayload{Operator: "-"})},
				{Op: string(ir.OpTailCallDirect), Payload: testPayload(ir.CallPayload{Function: "fn.count", ArgCount: 1, ResultCount: 1})},
				{Op: string(ir.OpLabel), Payload: testPayload(ir.LabelPayload{Label: "done"})},
				{Op: string(ir.OpLoadLocal), Payload: testPayload(ir.LocalPayload{Local: "local.n"})},
				{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
			},
		},
		{
			ID: "fn.main", Signature: testSignature("function() Int64"),
			Instructions: []ir.Instruction{
				{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "c.depth"})},
				{Op: string(ir.OpCallDirect), Payload: testPayload(ir.CallPayload{Function: "fn.count", ArgCount: 1, ResultCount: 1})},
				{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
			},
		},
	}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	vm, err := loadTestEngineWithOptions(artifact, InstanceOptions{Limits: Limits{MaxCallDepth: 4}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatal(err)
	}
	requireValues(t, result.Values, newVMValue("Int64", int64(0)))
}

func TestVMRunsConditionalJump(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.true", Type: testType("Bool"), Value: json.RawMessage(`true`)},
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.true"}`),
		}, {
			Op:      string(ir.OpJumpIf),
			Payload: json.RawMessage(`{"label":"truthy"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}, {
			Op:      string(ir.OpLabel),
			Payload: json.RawMessage(`{"label":"truthy"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	result, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("run export failed: %v", err)
	}
	requireValues(t, result.Values, newVMValue("Int64", int64(1)))
}

func TestVMRunsBinaryAndUnaryOperators(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.two", Type: testType("Int64"), Value: json.RawMessage(`2`)},
		{ID: "c.three", Type: testType("Int64"), Value: json.RawMessage(`3`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Bool"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.two"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.three"}`),
		}, {
			Op:      string(ir.OpBinary),
			Payload: json.RawMessage(`{"operator":"+"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.three"}`),
		}, {
			Op:      string(ir.OpBinary),
			Payload: json.RawMessage(`{"operator":">"}`),
		}, {
			Op:      string(ir.OpUnary),
			Payload: json.RawMessage(`{"operator":"!"}`),
		}, {
			Op:      string(ir.OpUnary),
			Payload: json.RawMessage(`{"operator":"!"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	result, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("run export failed: %v", err)
	}
	requireValues(t, result.Values, newVMValue("Bool", true))
}

func TestVMStoresAndLoadsGlobal(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.value", Type: testType("Int64"), Value: json.RawMessage(`7`)}}
	artifact.Globals = []ir.Global{{ID: "global.value", Type: testType("Int64")}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.value"}`),
		}, {
			Op:      string(ir.OpStoreGlobal),
			Payload: json.RawMessage(`{"global":"global.value"}`),
		}, {
			Op:      string(ir.OpLoadGlobal),
			Payload: json.RawMessage(`{"global":"global.value"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	result, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("run export failed: %v", err)
	}
	requireValues(t, result.Values, newVMValue("Int64", int64(7)))
}

func TestVMRunModuleExportRunsRootInitOnce(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
	}
	artifact.Globals = []ir.Global{{ID: "global.count", Type: testType("Int64")}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.init",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpLoadGlobal),
			Payload: json.RawMessage(`{"global":"global.count"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpBinary),
			Payload: json.RawMessage(`{"operator":"+"}`),
		}, {
			Op:      string(ir.OpStoreGlobal),
			Payload: json.RawMessage(`{"global":"global.count"}`),
		}},
	}, {
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpLoadGlobal),
			Payload: json.RawMessage(`{"global":"global.count"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	first, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("first run export failed: %v", err)
	}
	second, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("second run export failed: %v", err)
	}
	requireValues(t, first.Values, newVMValue("Int64", int64(1)))
	requireValues(t, second.Values, newVMValue("Int64", int64(1)))
}
