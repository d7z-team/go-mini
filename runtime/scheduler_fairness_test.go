package runtime

import (
	"encoding/json"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestSchedulerRotatesRunnableTasks(t *testing.T) {
	artifact := ir.NewArtifact("scheduler/fairness", "main")
	artifact.Constants = []ir.Constant{{ID: "const.true", Type: testType("Bool"), Value: json.RawMessage(`true`)}}
	artifact.Globals = []ir.Global{
		{ID: "global.ready", Type: testType("Bool")},
		{ID: "global.observed", Type: testType("Bool")},
	}
	child := make([]ir.Instruction, 0, taskInstructionQuantum*2+4)
	for range taskInstructionQuantum * 2 {
		child = append(child,
			ir.Instruction{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
			ir.Instruction{Op: string(ir.OpPop)},
		)
	}
	child = append(child,
		ir.Instruction{Op: string(ir.OpLoadGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.ready"})},
		ir.Instruction{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.observed"})},
		ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
	)
	artifact.Functions = []ir.Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpMakeClosure), Payload: testPayload(ir.ClosurePayload{Function: "fn.child"})},
			{Op: string(ir.OpSpawn), Payload: testPayload(ir.CallPayload{})},
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.true"})},
			{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.ready"})},
		},
	}, {
		ID: "fn.child", RevisionLocal: true,
		Signature: testSignature("function() Void"), Instructions: child,
	}}
	for range taskInstructionQuantum * 2 {
		artifact.Functions[0].Instructions = append(artifact.Functions[0].Instructions,
			ir.Instruction{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
			ir.Instruction{Op: string(ir.OpPop)},
		)
	}
	artifact.Functions[0].Instructions = append(artifact.Functions[0].Instructions,
		ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
	)
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runTestModuleExport(vm, "Main"); err != nil {
		t.Fatal(err)
	}
	observed := vm.rootModule().state.globals["global.observed"].load()
	if value, ok := observed.Data.(bool); !ok || !value {
		t.Fatalf("child observed ready = %#v", observed)
	}
}

func TestSchedulerDoesNotRotateInsideNoSwitchFunction(t *testing.T) {
	artifact := ir.NewArtifact("scheduler/no-switch", "main")
	artifact.Constants = []ir.Constant{
		{ID: "const.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
		{ID: "const.two", Type: testType("Int64"), Value: json.RawMessage(`2`)},
	}
	artifact.Globals = []ir.Global{
		{ID: "global.phase", Type: testType("Int64")},
		{ID: "global.observed", Type: testType("Int64")},
	}
	worker := []ir.Instruction{
		{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.one"})},
		{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.phase"})},
	}
	for range taskInstructionQuantum * 2 {
		worker = append(worker,
			ir.Instruction{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
			ir.Instruction{Op: string(ir.OpPop)},
		)
	}
	worker = append(worker,
		ir.Instruction{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.two"})},
		ir.Instruction{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.phase"})},
		ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
	)
	artifact.Functions = []ir.Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpMakeClosure), Payload: testPayload(ir.ClosurePayload{Function: "fn.worker"})},
			{Op: string(ir.OpSpawn), Payload: testPayload(ir.CallPayload{})},
			{Op: string(ir.OpLoadGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.phase"})},
			{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.observed"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
		},
	}, {
		ID: "fn.worker", RevisionLocal: true, NoSwitch: true,
		Signature: testSignature("function() Void"), Instructions: worker,
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runTestModuleExport(vm, "Main"); err != nil {
		t.Fatal(err)
	}
	observed := vm.rootModule().state.globals["global.observed"].load()
	value, valueErr := asInt64(observed)
	if valueErr != nil || value != 2 {
		t.Fatalf("task observed phase = %#v, want 2", observed)
	}
}
