package bytecode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateArtifactRejectsStackUnderflow(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op: string(OpPop),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "stack underflow") {
		t.Fatalf("expected stack underflow error, got %v", err)
	}
}

func TestValidateArtifactRejectsUnbalancedStack(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Constants = []Constant{{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)}}
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "stack not balanced") {
		t.Fatalf("expected stack not balanced error, got %v", err)
	}
}

func TestAnalyzeFunctionReportsMaxStack(t *testing.T) {
	fn := Function{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []Instruction{{
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op: string(OpPop),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}

	analysis, err := AnalyzeFunction(fn)
	if err != nil {
		t.Fatalf("AnalyzeFunction failed: %v", err)
	}
	if analysis.MaxStack != 2 {
		t.Fatalf("expected max stack 2, got %d", analysis.MaxStack)
	}
}

func TestValidateArtifactRejectsIncorrectMaxStack(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Constants = []Constant{{ID: "c.answer", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		MaxStack:  2,
		Instructions: []Instruction{{
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.answer"}`),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "max stack mismatch") {
		t.Fatalf("expected max stack mismatch, got %v", err)
	}
}

func TestAnalyzeFunctionPreservesOuterStackAcrossJump(t *testing.T) {
	fn := Function{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Locals: []Local{
			{ID: "local.cond", Type: testType("Bool")},
			{ID: "local.logical", Type: testType("Bool")},
		},
		Instructions: []Instruction{
			{Op: string(OpConst)},
			{Op: string(OpLoadLocal), Payload: testPayload(LocalPayload{Local: "local.cond"})},
			{Op: string(OpStoreLocal), Payload: testPayload(LocalPayload{Local: "local.logical"})},
			{Op: string(OpLoadLocal), Payload: testPayload(LocalPayload{Local: "local.logical"})},
			{Op: string(OpJumpIf), Payload: testPayload(JumpPayload{Label: "right"})},
			{Op: string(OpJump), Payload: testPayload(JumpPayload{Label: "end"})},
			{Op: string(OpLabel), Payload: testPayload(LabelPayload{Label: "right"})},
			{Op: string(OpConst)},
			{Op: string(OpStoreLocal), Payload: testPayload(LocalPayload{Local: "local.logical"})},
			{Op: string(OpLabel), Payload: testPayload(LabelPayload{Label: "end"})},
			{Op: string(OpLoadLocal), Payload: testPayload(LocalPayload{Local: "local.logical"})},
			{Op: string(OpPop)},
			{Op: string(OpPop)},
			{Op: string(OpReturn), Payload: testPayload(ReturnPayload{ResultCount: 0})},
		},
	}

	analysis, err := AnalyzeFunction(fn)
	if err != nil {
		t.Fatalf("AnalyzeFunction failed for nested short-circuit stack: %v", err)
	}
	if analysis.MaxStack != 2 {
		t.Fatalf("expected max stack 2, got %d", analysis.MaxStack)
	}
}

func TestAnalyzeFunctionTracksStackGrowth(t *testing.T) {
	fn := Function{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []Instruction{{
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op: string(OpPop),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}

	analysis, err := AnalyzeFunction(fn)
	if err != nil {
		t.Fatalf("AnalyzeFunction failed: %v", err)
	}
	if analysis.MaxStack != 2 {
		t.Fatalf("expected max stack 2, got %d", analysis.MaxStack)
	}
}

func TestValidateArtifactAcceptsCallInterfaceStack(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Constants = []Constant{{ID: "c.delta", Type: testType("Int64"), Value: json.RawMessage(`2`)}}
	setTestNamedTypes(&artifact, []testNamedType{{Name: "Adder", Type: testType("interface{Add:function(Int64) Int64}")}})
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Locals:    []Local{{ID: "local.a", Type: testType("Adder")}},
		Instructions: []Instruction{{
			Op:      string(OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.a"}`),
		}, {
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.delta"}`),
		}, {
			Op:      string(OpCallInterface),
			Payload: testPayload(CallInterfacePayload{InterfaceType: testType("Adder"), Method: "Add", ArgCount: 1, ResultCount: 1}),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}

	if err := testValidateArtifact(&artifact); err != nil {
		t.Fatalf("ValidateArtifact failed: %v", err)
	}
	analysis, err := AnalyzeFunction(artifact.Functions[0])
	if err != nil {
		t.Fatalf("AnalyzeFunction failed: %v", err)
	}
	if analysis.MaxStack != 2 {
		t.Fatalf("expected max stack 2, got %d", analysis.MaxStack)
	}
	disassembly, err := Disassemble(&artifact)
	if err != nil {
		t.Fatalf("Disassemble failed: %v", err)
	}
	if !strings.Contains(disassembly, "call_interface") || !strings.Contains(disassembly, `"interface_type":{"kind":4`) {
		t.Fatalf("expected call_interface in disassembly, got:\n%s", disassembly)
	}
}

func TestValidateArtifactRejectsInvalidCallInterfacePayload(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpCallInterface),
			Payload: testPayload(CallInterfacePayload{InterfaceType: testType("Adder"), ArgCount: 0}),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "missing method") {
		t.Fatalf("expected missing method error, got %v", err)
	}
}

func TestValidateArtifactAcceptsLoadIndexOKStack(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Constants = []Constant{
		{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"k"`)},
		{ID: "c.value", Type: testType("Int64"), Value: json.RawMessage(`42`)},
	}
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.value"}`),
		}, {
			Op:      string(OpMakeMap),
			Payload: testPayload(MakeMapPayload{Type: testType("Map<String, Int64>"), EntryCount: 1}),
		}, {
			Op:      string(OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op: string(OpLoadIndexOK),
		}, {
			Op: string(OpPop),
		}, {
			Op: string(OpPop),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":0}`),
		}},
	}}

	if err := testValidateArtifact(&artifact); err != nil {
		t.Fatalf("ValidateArtifact failed: %v", err)
	}
}
