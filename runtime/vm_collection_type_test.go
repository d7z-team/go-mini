package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestVMMakeSequenceRejectsWrongElementType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpMakeSequence),
			Payload: testArrayPayload("Slice<User>", 1),
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
	if err == nil || !strings.Contains(err.Error(), "array element 0") || !strings.Contains(err.Error(), "String is not example/module.User") {
		t.Fatalf("expected array literal type error, got %v", err)
	}
}

func TestVMMakeMapRejectsWrongValueType(t *testing.T) {
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
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.key"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpMakeMap),
			Payload: testMapPayload("Map<String, User>", 1),
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
	if err == nil || !strings.Contains(err.Error(), "map value 0") || !strings.Contains(err.Error(), "String is not example/module.User") {
		t.Fatalf("expected map literal type error, got %v", err)
	}
}

func TestVMAppendRejectsWrongElementType(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	setRuntimeTestNamedTypes(&artifact, []testNamedType{{
		Name: "User",
		Type: testType("struct{Name:String}"),
		Fields: []testTypeField{{
			Name: "Name",
			Type: testType("String"),
		}},
	}})
	artifact.Constants = []ir.Constant{{ID: "c.bad", Type: testType("String"), Value: json.RawMessage(`"bad"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpMakeSequence),
			Payload: testArrayPayload("Slice<User>", 0),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpAppend),
			Payload: json.RawMessage(`{"count":1}`),
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
	if err == nil || !strings.Contains(err.Error(), "append element 0") || !strings.Contains(err.Error(), "String is not example/module.User") {
		t.Fatalf("expected append element type error, got %v", err)
	}
}

func TestVMAppendExpandsEveryUTF8Byte(t *testing.T) {
	artifact := ir.NewArtifact("example/module", "main")
	artifact.Constants = []ir.Constant{{ID: "c.text", Type: testType("String"), Value: json.RawMessage(`"α"`)}}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Slice<Uint8>"),
		Instructions: []ir.Instruction{{
			Op: string(ir.OpMakeSequence), Payload: testArrayPayload("Slice<Uint8>", 0),
		}, {
			Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"c.text"}`),
		}, {
			Op: string(ir.OpAppend), Payload: testPayload(ir.CountPayload{Count: 1, Expand: true}),
		}, {
			Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1}),
		}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}

	engine, err := loadTestEngine(artifact)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runTestModuleExport(engine, "Main")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Values) != 1 {
		t.Fatalf("result = %#v", result.Values)
	}
	slice, ok := result.Values[0].Data.(*vmSlice)
	if !ok || !slice.ByteBacked || string(slice.ByteBacking[slice.Start:slice.Start+slice.Len]) != "α" {
		t.Fatalf("expanded bytes = %#v", result.Values[0])
	}
}

func TestVMSliceStoreAndClearPreserveElementMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		clear bool
		want  string
	}{
		{name: "store", want: "Ada"},
		{name: "clear", clear: true, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
				{ID: "c.name", Type: testType("String"), Value: json.RawMessage(`"Ada"`)},
			}
			instructions := []ir.Instruction{
				{Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"c.one"}`)},
				{Op: string(ir.OpMakeSlice), Payload: testMakeSlicePayload("Slice<User>")},
				{Op: string(ir.OpStoreLocal), Payload: json.RawMessage(`{"local":"local.users"}`)},
				{Op: string(ir.OpLoadLocal), Payload: json.RawMessage(`{"local":"local.users"}`)},
				{Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"c.zero"}`)},
				{Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"c.name"}`)},
				{Op: string(ir.OpMakeStruct), Payload: testStructPayload("User", "Name")},
				{Op: string(ir.OpStoreIndex)},
			}
			if tc.clear {
				instructions = append(instructions,
					ir.Instruction{Op: string(ir.OpLoadLocal), Payload: json.RawMessage(`{"local":"local.users"}`)},
					ir.Instruction{Op: string(ir.OpClear)},
				)
			}
			instructions = append(instructions,
				ir.Instruction{Op: string(ir.OpLoadLocal), Payload: json.RawMessage(`{"local":"local.users"}`)},
				ir.Instruction{Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"c.zero"}`)},
				ir.Instruction{Op: string(ir.OpLoadIndex)},
				ir.Instruction{Op: string(ir.OpLoadField), Payload: json.RawMessage(`{"field":"Name"}`)},
				ir.Instruction{Op: string(ir.OpReturn), Payload: json.RawMessage(`{"result_count":1}`)},
			)
			artifact.Functions = []ir.Function{{
				ID:           "fn.main",
				Signature:    testSignature("function() String"),
				Locals:       []ir.Local{{ID: "local.users", Type: testType("Slice<User>")}},
				Instructions: instructions,
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
			requireValues(t, result.Values, newVMValue("String", tc.want))
		})
	}
}

func TestVMCopySliceUsesElementTypeMetadata(t *testing.T) {
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
		{ID: "c.name", Type: testType("String"), Value: json.RawMessage(`"Ada"`)},
	}
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() String"),
		Locals: []ir.Local{
			{ID: "local.dst", Type: testType("Slice<User>")},
			{ID: "local.src", Type: testType("Slice<User>")},
		},
		Instructions: []ir.Instruction{{
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpMakeSlice),
			Payload: testMakeSlicePayload("Slice<User>"),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.dst"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.one"}`),
		}, {
			Op:      string(ir.OpMakeSlice),
			Payload: testMakeSlicePayload("Slice<User>"),
		}, {
			Op:      string(ir.OpStoreLocal),
			Payload: json.RawMessage(`{"local":"local.src"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.src"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
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
			Payload: json.RawMessage(`{"local":"local.dst"}`),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.src"}`),
		}, {
			Op: string(ir.OpCopy),
		}, {
			Op: string(ir.OpPop),
		}, {
			Op:      string(ir.OpLoadLocal),
			Payload: json.RawMessage(`{"local":"local.dst"}`),
		}, {
			Op:      string(ir.OpConst),
			Payload: json.RawMessage(`{"constant":"c.zero"}`),
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

func TestVMCopySliceRejectsWrongElementType(t *testing.T) {
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
			Payload: json.RawMessage(`{"constant":"c.bad"}`),
		}, {
			Op:      string(ir.OpMakeSequence),
			Payload: testArrayPayload("Slice<String>", 1),
		}, {
			Op: string(ir.OpCopy),
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
	if err == nil || !strings.Contains(err.Error(), "copy element 0") || !strings.Contains(err.Error(), "String is not example/module.User") {
		t.Fatalf("expected copy element type error, got %v", err)
	}
}
