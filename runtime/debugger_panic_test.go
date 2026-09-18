package runtime

import (
	"encoding/json"
	"errors"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestDebuggerSchemaPanic(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.message", Type: testType("String"), Value: json.RawMessage(`"failed"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Locals:    []ir.Local{{ID: "local.msg", Type: testType("String")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.message"}`),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.msg"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.msg"}`),
		}, {
			Op: string(ir.OpPanic),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.main", pc: 3, line: 9, column: 2})

	debugger := NewDebugger()
	vm, err := loadTestEngineWithOptions(artifact, InstanceOptions{Debugger: debugger})
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil {
		t.Fatal("expected panic error")
	}
	var panicErr panicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("expected PanicError, got %T: %v", err, err)
	}
	events := debugger.Events()
	if len(events) != 1 {
		t.Fatalf("expected one schema event, got %#v", events)
	}
	event := events[0]
	requireDebugEvent(t, event, EventPanic, "fn.main", 3, 9)
	if event.Panic == nil {
		t.Fatal("expected panic value")
	}
	requireDebugString(t, *event.Panic, "failed")
	requireDebugString(t, event.Frame.Locals[0].Value, "failed")
}

func TestDebuggerSchemaDeferredReturn(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.answer", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeClosure),
			Payload: json.RawMessage(`{"function":"fn.cleanup"}`),
		}, {
			Op: string(ir.OpDeferPush),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.answer"}`),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}, {
		ID:        "fn.cleanup",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":0}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.cleanup", pc: 0, line: 8, column: 2})

	debugger := NewDebugger()
	vm, err := loadTestEngineWithOptions(artifact, InstanceOptions{Debugger: debugger})
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	setTestBreakpoints(t, vm, "example/module", "main.mgo", 8)
	handle, err := startTestExecution(vm, "Main")
	if err != nil {
		t.Fatalf("start execution failed: %v", err)
	}
	if handle.State() != ExecutionPaused {
		t.Fatalf("expected deferred pause, got %s", handle.State())
	}
	event, ok := handle.PauseEvent()
	if !ok {
		t.Fatal("expected schema deferred pause event")
	}
	requireDebugEventWithContext(t, event, EventBreakpoint, "fn.cleanup", 0, 8, 1)
	if len(event.Stack) != 2 || event.Stack[1].FunctionID != "fn.main" || event.Stack[1].ExecutionContextID != 1 {
		t.Fatalf("expected deferred stack to include owner frame in context 1, got %#v", event.Stack)
	}
	result, err := continueTestExecution(handle)
	if err != nil {
		t.Fatalf("Continue failed: %v", err)
	}
	requireValues(t, result.Values, newVMValue("Int64", int64(42)))
}

func TestDebuggerSchemaDeferredRecover(t *testing.T) {
	artifact := deferredPanicTestArtifact([]ir.Instruction{
		{Op: string(ir.OpRecover)},
		{Op: string(ir.OpPop)},
		{Op: string(ir.OpReturn), Payload: json.RawMessage(`{"result_count":0}`)},
	})
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.cleanup", pc: 0, line: 8, column: 2})

	debugger := NewDebugger()
	vm, err := loadTestEngineWithOptions(artifact, InstanceOptions{Debugger: debugger})
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	setTestBreakpoints(t, vm, "example/module", "main.mgo", 8)
	handle, err := startTestExecution(vm, "Main")
	if err != nil {
		t.Fatalf("start execution failed: %v", err)
	}
	event, ok := handle.PauseEvent()
	if !ok {
		t.Fatal("expected schema deferred recover pause event")
	}
	requireDebugEventWithContext(t, event, EventBreakpoint, "fn.cleanup", 0, 8, 1)
	result, err := continueTestExecution(handle)
	if err != nil {
		t.Fatalf("Continue failed: %v", err)
	}
	requireValues(t, result.Values)
	if events := debugger.Events(); len(events) != 1 || events[0].Kind != EventBreakpoint {
		t.Fatalf("expected only recovered breakpoint event, got %#v", events)
	}
}
