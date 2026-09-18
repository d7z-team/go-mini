package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestVMRunsCompositeAccess(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
		{ID: "c.answer", Type: testType("Int64"), Value: json.RawMessage(`42`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.answer"}`),
		}, {
			Op:      string(ir.OpMakeSequence),
			Payload: testArrayPayload("Slice<Int64>", 2),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
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
	requireValues(t, result.Values, newVMValue("Int64", int64(42)))
}

func TestVMRunsCompositeMutation(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
		{ID: "c.answer", Type: testType("Int64"), Value: json.RawMessage(`42`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Locals:    []ir.Local{{ID: "local.arr", Type: testType("Slice<Int64>")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpMakeSequence),
			Payload: testArrayPayload("Slice<Int64>", 2),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.arr"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.arr"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.answer"}`),
		}, {
			Op: string(ir.OpStoreIndex),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.arr"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
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
	requireValues(t, result.Values, newVMValue("Int64", int64(42)))
}

func TestVMRunsStructMember(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "Point",
		Type: testType("struct{X:Int64}"),
		Fields: []testTypeField{{
			Name: "X",
			Type: testType("Int64"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.answer", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.answer"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("Point", "X"),
		}, {
			Op:      string(ir.OpLoadField),
			Payload: json.RawMessage(`{"field":"X"}`),
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
	requireValues(t, result.Values, newVMValue("Int64", int64(42)))
}

func TestVMZeroNamedStructUsesTypeFieldMetadata(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{ID:Int64 `json:\"id\"`,Name:String `json:\"name\"`}"),
		Fields: []testTypeField{{
			Name: "ID",
			Type: testType("Int64"),
			Tag:  `json:"id"`,
		}, {
			Name: "Name",
			Type: testType("String"),
			Tag:  `json:"name"`,
		}},
	}})
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpZero),
			Payload: testTypePayload("User"),
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

func TestVMZeroNamedStructMetadataHandlesRecursiveType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "Node",
		Type: testType("struct{Next:Ptr<Node>,Label:String}"),
		Fields: []testTypeField{{
			Name: "Next",
			Type: testType("Ptr<Node>"),
		}, {
			Name: "Label",
			Type: testType("String"),
		}},
	}})

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	value := vm.rootModule().zeroValue("Node")
	next, err := loadFieldValue(vm.rootModule(), value, "Next")
	if err != nil {
		t.Fatal(err)
	}
	if next.Type.String() != "Ptr<example/module.Node>" || next.Data != nil {
		t.Fatalf("expected recursive pointer field to be nil, got %#v", next)
	}
	label, err := loadFieldValue(vm.rootModule(), value, "Label")
	if err != nil {
		t.Fatal(err)
	}
	requireValues(t, []vmValue{label}, newVMValue("String", ""))
}

func TestVMNamedStructMemberReadUsesTypeFieldMetadata(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{ID:Int64,Name:String}"),
		Fields: []testTypeField{{
			Name: "ID",
			Type: testType("Int64"),
		}, {
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.id", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.id"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("User", "ID"),
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

func TestVMNamedStructMakeStructRejectsUnknownField(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.age", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.age"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("User", "Age"),
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
	if err == nil || !strings.Contains(err.Error(), `unknown field "Age" for example/module.User`) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestVMNamedStructMakeStructRejectsWrongFieldType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpMakeStruct),
			Payload: testStructPayload("User", "Name"),
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
	if err == nil || !strings.Contains(err.Error(), `field "Name"`) || !strings.Contains(err.Error(), "Int64 is not String") {
		t.Fatalf("expected field type error, got %v", err)
	}
}

func TestVMNamedStructStoreFieldUsesTypeFieldMetadata(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{ID:Int64,Name:String}"),
		Fields: []testTypeField{{
			Name: "ID",
			Type: testType("Int64"),
		}, {
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.name", Type: testType("String"), Value: json.RawMessage(`"Ada"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Locals:    []ir.Local{{ID: "local.user", Type: testType("User")}},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpZero),
			Payload: testTypePayload("User"),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: testPayload(ir.LocalPayload{Local: "local.user"}),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: testPayload(ir.LocalPayload{Local: "local.user"}),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.name"}`),
		}, {
			Op:      string(ir.OpStoreField),
			Payload: json.RawMessage(`{"field":"Name"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: testPayload(ir.LocalPayload{Local: "local.user"}),
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

func TestVMNamedStructStoreFieldRejectsWrongType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("Int64"), Value: json.RawMessage(`42`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpZero),
			Payload: testTypePayload("User"),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpStoreField),
			Payload: json.RawMessage(`{"field":"Name"}`),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	vm, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatalf("load test engine failed: %v", err)
	}
	_, err = runTestModuleExport(vm, "Main")
	if err == nil || !strings.Contains(err.Error(), `field "Name"`) || !strings.Contains(err.Error(), "Int64 is not String") {
		t.Fatalf("expected field type error, got %v", err)
	}
}
