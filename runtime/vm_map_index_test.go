package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestVMSetIndexMapUsesKeyValueTypes(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{
		{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"u"`)},
		{ID: "c.name", Type: testType("String"), Value: json.RawMessage(`"Ada"`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Locals:    []ir.Local{{ID: "local.users", Type: testType("Map<String, User>")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 0),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.users"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.users"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.name"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("User", "Name"),
		}, {
			Op: string(ir.OpStoreIndex),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.users"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op: string(ir.OpLoadIndex),
		}, {
			Op:      string(ir.OpLoadField),
			Payload: json.RawMessage(`{"field":"Name"}`),
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
	requireValues(t, result.Values, newVMValue("String", "Ada"))
}

func TestVMSetIndexMapRejectsWrongValueType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{
		{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"u"`)},
		{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op: string(ir.OpStoreIndex),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "map value") || !strings.Contains(err.Error(), "String is not example/module.User") {
		t.Fatalf("expected map value type error, got %v", err)
	}
}

func TestVMMapIndexRejectsWrongKeyType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("Int64"), Value: json.RawMessage(`1`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op: string(ir.OpLoadIndex),
		}, {
			Op: string(ir.OpPop),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "map key") || !strings.Contains(err.Error(), "Int64 is not String") {
		t.Fatalf("expected map key type error, got %v", err)
	}
}

func TestVMDeleteMapRejectsWrongKeyType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("Int64"), Value: json.RawMessage(`1`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op: string(ir.OpDelete),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), "map key") || !strings.Contains(err.Error(), "Int64 is not String") {
		t.Fatalf("expected map key type error, got %v", err)
	}
}

func TestVMMapKeysUsesMapKeyType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
		{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"u"`)},
		{ID: "c.name", Type: testType("String"), Value: json.RawMessage(`"Ada"`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Locals:    []ir.Local{{ID: "local.keys", Type: testType("Slice<String>")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.name"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("User", "Name"),
		}, {
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 1),
		}, {
			Op: string(ir.OpMapKeys),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.keys"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.keys"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op: string(ir.OpLoadIndex),
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
	requireValues(t, result.Values, newVMValue("String", "u"))
}

func TestVMMapIndexMissingNamedStructReturnsZeroValue(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"missing"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op: string(ir.OpLoadIndex),
		}, {
			Op:      string(ir.OpLoadField),
			Payload: json.RawMessage(`{"field":"Name"}`),
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
	requireValues(t, result.Values, newVMValue("String", ""))
}

func TestVMMapIndexMissingPrimitiveReturnsZeroValue(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"missing"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, Int64>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op: string(ir.OpLoadIndex),
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
	requireValues(t, result.Values, newVMValue("Int64", int64(0)))
}

func TestVMLoadIndexOKReturnsValueAndPresence(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"u"`)},
		{ID: "c.value", Type: testType("Int64"), Value: json.RawMessage(`42`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() tuple(Int64, Bool)"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.value"}`),
		}, {
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, Int64>", 1),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op: string(ir.OpLoadIndexOK),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":2}`),
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
	requireValues(t, result.Values, newVMValue("Int64", int64(42)), newVMValue("Bool", true))
}

func TestVMLoadIndexOKMissingReturnsZeroValueAndFalse(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.key", Type: testType("String"), Value: json.RawMessage(`"missing"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() tuple(Int64, Bool)"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, Int64>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op: string(ir.OpLoadIndexOK),
		}, {
			Op:      string(ir.OpReturn),
			Payload: json.RawMessage(`{"result_count":2}`),
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
	requireValues(t, result.Values, newVMValue("Int64", int64(0)), newVMValue("Bool", false))
}
