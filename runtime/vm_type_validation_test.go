package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestVMStoreLocalRejectsWrongDeclaredType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Locals:    []ir.Local{{ID: "local.value", Type: testType("Int64")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.value"}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "store local local.value") || !strings.Contains(err.Error(), "String is not Int64") {
		t.Fatalf("expected local slot type error, got %v", err)
	}
}

func TestVMStoreGlobalRejectsWrongDeclaredType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)}}
	artifact.Globals = []ir.Global{{ID: "global.value", Type: testType("Int64")}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpStoreGlobal),
			Payload: json.RawMessage(`{"global":"global.value"}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "store global global.value") || !strings.Contains(err.Error(), "String is not Int64") {
		t.Fatalf("expected global slot type error, got %v", err)
	}
}

func TestVMFunctionArgumentRejectsWrongDeclaredType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.accept",
		Signature: testSignature("function(Int64) Void"),
		Locals:    []ir.Local{{ID: "local.value", Type: testType("Int64")}},
	}, {
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpCallDirect),
			Payload: json.RawMessage(`{"function":"fn.accept","arg_count":1,"result_count":0}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "argument local.value") || !strings.Contains(err.Error(), "String is not Int64") {
		t.Fatalf("expected argument type error, got %v", err)
	}
}

func TestVMReturnRejectsWrongDeclaredType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
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
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "return value 0") || !strings.Contains(err.Error(), "String is not Int64") {
		t.Fatalf("expected return type error, got %v", err)
	}
}

func TestVMFunctionFallthroughRejectsMissingReturnValue(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "return value count mismatch") {
		t.Fatalf("expected missing return value error, got %v", err)
	}
}

func TestVMReturnNamedStructCopiesValue(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.name", Type: testType("String"), Value: json.RawMessage(`"Ada"`)}}
	artifact.Globals = []ir.Global{{ID: "global.user", Type: testType("User")}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.init",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.name"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("User", "Name"),
		}, {
			Op:      string(ir.OpStoreGlobal),
			Payload: json.RawMessage(`{"global":"global.user"}`),
		}},
	}, {
		ID:        "fn.main",
		Signature: testSignature("function() User"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpLoadGlobal),
			Payload: json.RawMessage(`{"global":"global.user"}`),
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
	if _, err := storeFieldValue(vm.rootModule(), first.Values[0], "Name", newVMValue("String", "Eve")); err != nil {
		t.Fatalf("mutate returned struct: %v", err)
	}
	second, err := runTestModuleExport(vm, "Main")
	if err != nil {
		t.Fatalf("second run export failed: %v", err)
	}
	got, err := loadFieldValue(vm.rootModule(), second.Values[0], "Name")
	if err != nil {
		t.Fatalf("load returned field: %v", err)
	}
	if got.Type.String() != "String" || got.Data != "Ada" {
		t.Fatalf("return value mutation leaked into VM global, got %#v", got)
	}
}
