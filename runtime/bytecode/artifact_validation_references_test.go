package bytecode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateArtifactRejectsUnknownAddressLocal(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Ptr<Int64>"),
		Instructions: []Instruction{{
			Op:      string(OpAddressOf),
			Payload: json.RawMessage(`{"kind":"local","local":"local.missing"}`),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "unknown local") {
		t.Fatalf("expected unknown local error, got %v", err)
	}
}

func TestValidateArtifactRejectsRebindOnLoadLocal(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Locals:    []Local{{ID: "local.value", Type: testType("Int64")}},
		Instructions: []Instruction{{
			Op:      string(OpLoadLocal),
			Payload: testPayload(LocalPayload{Local: "local.value", Rebind: true}),
		}, {
			Op:      string(OpReturn),
			Payload: testPayload(ReturnPayload{ResultCount: 1}),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "rebind is only valid for store_local") {
		t.Fatalf("expected invalid rebind payload error, got %v", err)
	}
}

func TestValidateArtifactRejectsUnknownDirectCall(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpCallDirect),
			Payload: json.RawMessage(`{"function":"fn.missing","arg_count":0}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "unknown function id") {
		t.Fatalf("expected unknown function id error, got %v", err)
	}
}

func TestValidateArtifactRejectsDirectCallShapeMismatch(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op: string(OpCallDirect), Payload: testPayload(CallPayload{Function: "fn.target", ResultCount: 1}),
		}},
	}, {
		ID: "fn.target", Signature: testSignature("function(Int) Void"),
	}}
	if err := testValidateArtifact(&artifact); err == nil || !strings.Contains(err.Error(), "call argument count mismatch") {
		t.Fatalf("ValidateArtifact() = %v, want call argument count mismatch", err)
	}
}

func TestValidateArtifactRejectsReturnShapeMismatch(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID: "fn.main", Signature: testSignature("function() Int"),
		Instructions: []Instruction{{
			Op: string(OpReturn), Payload: testPayload(ReturnPayload{}),
		}},
	}}
	if err := testValidateArtifact(&artifact); err == nil || !strings.Contains(err.Error(), "return result count mismatch") {
		t.Fatalf("ValidateArtifact() = %v, want return result count mismatch", err)
	}
}

func TestValidateArtifactAcceptsCrossModuleDirectCallRequirement(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Requirements = []Requirement{{Kind: "source", ModulePath: "example/lib"}}
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpCallDirect),
			Payload: json.RawMessage(`{"module_path":"example/lib","function":"method.Counter.Add","arg_count":0}`),
		}},
	}}

	if err := testValidateArtifact(&artifact); err != nil {
		t.Fatalf("ValidateArtifact failed: %v", err)
	}
}

func TestValidateArtifactAcceptsLocalModuleDirectCallPayload(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpCallDirect),
			Payload: json.RawMessage(`{"module_path":"example/module","function":"fn.helper","arg_count":0}`),
		}},
	}, {
		ID:        "fn.helper",
		Signature: testSignature("function() Void"),
	}}

	if err := testValidateArtifact(&artifact); err != nil {
		t.Fatalf("ValidateArtifact failed: %v", err)
	}
}

func TestValidateArtifactRejectsCrossModuleDirectCallWithoutRequirement(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpCallDirect),
			Payload: json.RawMessage(`{"module_path":"example/lib","function":"method.Counter.Add","arg_count":0}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "unknown module requirement") {
		t.Fatalf("expected unknown module requirement error, got %v", err)
	}
}

func TestValidateArtifactRejectsUnknownClosureFunction(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpMakeClosure),
			Payload: json.RawMessage(`{"function":"fn.missing"}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "unknown function id") {
		t.Fatalf("expected unknown function id error, got %v", err)
	}
}

func TestValidateArtifactAcceptsCrossModuleClosureRequirement(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Requirements = []Requirement{{Kind: "source", ModulePath: "example/lib"}}
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpMakeClosure),
			Payload: json.RawMessage(`{"module_path":"example/lib","function":"method.Counter.Add"}`),
		}, {
			Op: string(OpPop),
		}},
	}}

	if err := testValidateArtifact(&artifact); err != nil {
		t.Fatalf("ValidateArtifact failed: %v", err)
	}
}

func TestValidateArtifactRejectsCrossModuleClosureWithoutRequirement(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpMakeClosure),
			Payload: json.RawMessage(`{"module_path":"example/lib","function":"method.Counter.Add"}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "unknown module requirement") {
		t.Fatalf("expected unknown module requirement error, got %v", err)
	}
}

func TestValidateArtifactRejectsUnknownUpvalueSlot(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []Instruction{{
			Op:      string(OpLoadUpvalue),
			Payload: json.RawMessage(`{"upvalue":"up.missing"}`),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "unknown upvalue") {
		t.Fatalf("expected unknown upvalue error, got %v", err)
	}
}

func TestValidateArtifactRejectsClosureCaptureCountMismatch(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.child",
		Signature: testSignature("function() Int64"),
		Upvalues:  []Upvalue{{ID: "up.x", Type: testType("Int64")}},
		Instructions: []Instruction{{
			Op:      string(OpLoadUpvalue),
			Payload: json.RawMessage(`{"upvalue":"up.x"}`),
		}, {
			Op:      string(OpReturn),
			Payload: json.RawMessage(`{"result_count":1}`),
		}},
	}, {
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpMakeClosure),
			Payload: json.RawMessage(`{"function":"fn.child"}`),
		}, {
			Op: string(OpPop),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "capture count mismatch") {
		t.Fatalf("expected capture count mismatch error, got %v", err)
	}
}

func TestValidateArtifactRejectsSpawnResultCount(t *testing.T) {
	artifact := NewArtifact("example/module", "main")
	artifact.Functions = []Function{{
		ID:        "fn.child",
		Signature: testSignature("function() Void"),
	}, {
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{{
			Op:      string(OpMakeClosure),
			Payload: json.RawMessage(`{"function":"fn.child"}`),
		}, {
			Op:      string(OpSpawn),
			Payload: json.RawMessage(`{"arg_count":0,"result_count":1}`),
		}},
	}}

	err := testValidateArtifact(&artifact)
	if err == nil || !strings.Contains(err.Error(), "spawn must not produce results") {
		t.Fatalf("expected spawn result count error, got %v", err)
	}
}
