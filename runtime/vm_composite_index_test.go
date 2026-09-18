package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestVMMakeSliceUsesNamedStructTypeMetadata(t *testing.T) {
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
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpMakeSlice),
			Payload: testMakeSlicePayload("Slice<User>"),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
		}, {
			Op:      string(ir.OpLoadIndex),
			Payload: json.RawMessage(`{}`),
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

func TestVMSetIndexSliceRejectsWrongElementType(t *testing.T) {
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
		{ID: "c.one", Type: testType("Int64"), Value: json.RawMessage(`1`)},
		{ID: "c.zero", Type: testType("Int64"), Value: json.RawMessage(`0`)},
		{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpMakeSlice),
			Payload: testMakeSlicePayload("Slice<User>"),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
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
	if err == nil || !strings.Contains(err.Error(), "slice element") || !strings.Contains(err.Error(), "String is not example/module.User") {
		t.Fatalf("expected slice element type error, got %v", err)
	}
}
