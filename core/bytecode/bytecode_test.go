package bytecode

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/runtime"
)

func assertContainsAll(t *testing.T, text string, expected []string) {
	t.Helper()
	for _, sym := range expected {
		if !strings.Contains(text, sym) {
			t.Fatalf("expected %q in output, got:\n%s", sym, text)
		}
	}
}

func TestNewProgramInitializesStableHeader(t *testing.T) {
	prog := NewProgram()
	if prog.Format != FormatGoMiniBytecode {
		t.Fatalf("unexpected format: %s", prog.Format)
	}
	if prog.Version != CurrentVersion {
		t.Fatalf("unexpected version: %d", prog.Version)
	}
	if prog.OpcodeSet == "" {
		t.Fatal("expected opcode set")
	}
	if err := prog.Validate(); err != nil {
		t.Fatalf("expected valid program header: %v", err)
	}
}

func TestBytecodeJSONRoundTripValidatesHeader(t *testing.T) {
	prog := NewProgram()
	prog.Entry = []Instruction{{Op: "PUSH", Operand: "1"}}

	payload, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	decoded, err := UnmarshalJSON(payload)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Format != FormatGoMiniBytecode || decoded.Version != CurrentVersion {
		t.Fatalf("unexpected decoded header: %+v", decoded)
	}
	if len(decoded.Entry) != 1 || decoded.Entry[0].Op != "PUSH" {
		t.Fatalf("unexpected decoded instructions: %+v", decoded.Entry)
	}
}

func TestBytecodeJSONRejectsUnsupportedVersion(t *testing.T) {
	payload := []byte(`{"format":"go-mini-bytecode","version":999,"opcode_set":"runtime.opcode.v1","entry":[{"op":"PUSH","operand":"1"}]}`)
	_, err := UnmarshalJSON(payload)
	if err == nil {
		t.Fatal("expected unsupported version error")
	}
	if !strings.Contains(err.Error(), "unsupported bytecode version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDisassembleUsesNasmStyleMetadata(t *testing.T) {
	prog := NewProgram()
	prog.Globals = []Global{
		{
			Name: "counter",
			Instructions: []Instruction{
				{Op: "PUSH", Operand: "1", NodeID: "lit_1", Loc: &Location{File: "demo.go", Line: 3, Column: 12}, Comment: "literal"},
			},
		},
	}
	prog.Entry = []Instruction{{Op: "CALL", Operand: "main", Comment: "call main"}}
	prog.Executable = &runtime.PreparedProgram{
		Package:       "demo",
		Constants:     map[string]runtime.FFIConstValue{"Version": runtime.ConstString("v1")},
		ConstantTypes: map[string]runtime.RuntimeType{"Version": runtime.MustParseRuntimeType("String")},
		NamedTypes: map[string]runtime.RuntimeType{
			"Alias": runtime.MustParseRuntimeType("String"),
		},
		StructSchemas: map[string]*runtime.RuntimeStructSpec{
			"Payload": runtime.MustParseRuntimeStructSpec("Payload", runtime.StructOwnershipVMValue, "struct { Msg String; }"),
		},
		InterfaceSchemas: map[string]*runtime.RuntimeInterfaceSpec{
			"Reader": runtime.MustParseRuntimeInterfaceSpec("interface{Read() String;}"),
		},
		Globals: map[string]*runtime.PreparedGlobal{
			"counter": {
				Name:    "counter",
				Kind:    runtime.MustParseRuntimeType("Int64"),
				HasInit: true,
				InitPlan: []runtime.Task{{
					Op:     runtime.OpPush,
					Source: &runtime.SourceRef{ID: "lit_1", File: "demo.go", Line: 3, Col: 12},
					Data:   runtime.NewInt(1),
				}},
			},
			"pending": {Name: "pending", Kind: runtime.MustParseRuntimeType("Int64"), HasInit: false},
		},
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
			},
			"cleanup": {
				Name:        "cleanup",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
				BodyTasks:   []runtime.Task{{Op: runtime.OpReturn}},
			},
		},
	}

	asm := prog.Disassemble()
	expected := []string{
		"section .bss",
		"; package    demo",
		"section .note.go_mini",
		"; type Alias = String",
		"; struct Payload { Msg String }",
		"; interface Reader interface{Read() String;}",
		"global.pending: resq 1",
		"section .data",
		"global.counter: ; names=counter",
		"0000  PUSH",
		"node=lit_1",
		"demo.go:3:12",
		"section .text",
		"global _start",
		"_start:",
		"fn.cleanup: ; signature function() Void",
		"0000  RETURN",
		"fn.main: ; signature function() Void",
		"; no body",
	}
	assertContainsAll(t, asm, expected)
}

func TestDisassembleFullyExpandsPreparedSwitchBlocks(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Package: "demo",
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
				BodyTasks: []runtime.Task{
					{
						Op: runtime.OpSwitchTag,
						Data: &runtime.SwitchData{
							Init: []runtime.Task{
								{Op: runtime.OpPush, Data: &runtime.Var{TypeInfo: runtime.MustParseRuntimeType("Int64"), VType: runtime.TypeInt, I64: 1}},
							},
							Tag: []runtime.Task{
								{Op: runtime.OpLoadVar, Data: &runtime.LoadVarData{Name: "v"}},
							},
							Cases: []runtime.SwitchCaseData{
								{
									Exprs: [][]runtime.Task{
										{
											{Op: runtime.OpPush, Data: &runtime.Var{TypeInfo: runtime.MustParseRuntimeType("Int64"), VType: runtime.TypeInt, I64: 2}},
										},
										{
											{Op: runtime.OpPush, Data: &runtime.Var{TypeInfo: runtime.MustParseRuntimeType("Int64"), VType: runtime.TypeInt, I64: 3}},
										},
									},
									Body: []runtime.Task{
										{Op: runtime.OpReturn, Data: 0},
									},
								},
							},
							DefaultBody: []runtime.Task{
								{Op: runtime.OpPop},
							},
						},
					},
				},
			},
		},
	}

	asm := prog.Disassemble()
	expected := []string{
		"fn.main: ; signature function() Void",
		"SWITCH_TAG",
		"init->fn.main.L0000.init",
		"tag->fn.main.L0001.tag",
		"case_0_match_0->fn.main.L0002.case_0_match_0",
		"case_0_match_1->fn.main.L0003.case_0_match_1",
		"case_0->fn.main.L0004.case_0",
		"default->fn.main.L0005.default",
		"fn.main.L0000.init:",
		"fn.main.L0001.tag:",
		"fn.main.L0002.case_0_match_0:",
		"fn.main.L0003.case_0_match_1:",
		"fn.main.L0004.case_0:",
		"fn.main.L0005.default:",
		"0001  PUSH               1",
		"0003  PUSH               2",
		"0004  PUSH               3",
		"RETURN",
		"POP",
	}
	assertContainsAll(t, asm, expected)
}

func TestDisassembleFullyExpandsPreparedSelectBlocks(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Package: "demo",
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
				BodyTasks: []runtime.Task{
					{
						Op: runtime.OpSelect,
						Data: &runtime.SelectData{
							Cases: []runtime.SelectCaseData{
								{
									Kind:     runtime.SelectCommRecv,
									RecvName: "v",
									RecvType: runtime.MustParseRuntimeType("Int64"),
									Body: []runtime.Task{
										{Op: runtime.OpPush, Data: runtime.NewInt(1)},
									},
								},
								{
									Kind: runtime.SelectCommDefault,
									Body: []runtime.Task{
										{Op: runtime.OpReturn, Data: 0},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	prog.RefreshDisplayFromExecutable()
	if len(prog.Functions) != 1 || prog.Functions[0].Name != "main" {
		t.Fatalf("unexpected function preview: %+v", prog.Functions)
	}
	hasSelectLabel := false
	hasCasePush := false
	for _, inst := range prog.Functions[0].Instructions {
		if inst.Op == pseudoOpLabel && strings.Contains(inst.Operand, ".select_case_0_recv") {
			hasSelectLabel = true
		}
		if inst.Op == runtime.OpPush.String() && inst.Operand == "1" {
			hasCasePush = true
		}
	}
	if !hasSelectLabel || !hasCasePush {
		t.Fatalf("select case body was not expanded in preview: %+v", prog.Functions[0].Instructions)
	}

	asm := prog.Disassemble()
	expected := []string{
		"SELECT             cases=2",
		"select_case_0_recv->fn.main.L0000.select_case_0_recv",
		"select_case_1_default->fn.main.L0001.select_case_1_default",
		"fn.main.L0000.select_case_0_recv:",
		"fn.main.L0001.select_case_1_default:",
		"PUSH               1",
		"RETURN             0",
	}
	assertContainsAll(t, asm, expected)
}

func TestDisassembleIncludesEmbeddedModuleTasks(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Package: "main",
		Globals: map[string]*runtime.PreparedGlobal{},
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
			},
		},
		ModuleHashes: map[string]string{"lib": "hash-lib"},
		Modules: map[string]*runtime.PreparedProgram{
			"lib": {
				Package: "lib",
				Globals: map[string]*runtime.PreparedGlobal{
					"Value": {
						Name:    "Value",
						Kind:    runtime.MustParseRuntimeType("Int64"),
						HasInit: true,
					},
				},
				GlobalInitGroups: []*runtime.PreparedGlobalInit{
					{
						Names: []string{"Value"},
						InitPlan: []runtime.Task{
							{Op: runtime.OpDeclareInitVars, Data: &runtime.VarDeclData{
								Mode:       runtime.VarDeclInitPerBinding,
								ValueCount: 1,
								Bindings: []runtime.DeclareVarData{
									{Name: "Value", Kind: runtime.MustParseRuntimeType("Int64")},
								},
							}},
							{Op: runtime.OpPush, Data: runtime.NewInt(7)},
						},
					},
				},
				Functions: map[string]*runtime.PreparedFunction{
					"Run": {
						Name:        "Run",
						FunctionSig: runtime.MustParseRuntimeFuncSig("function() Int64"),
						BodyTasks: []runtime.Task{
							{Op: runtime.OpReturn, Data: 1},
							{Op: runtime.OpLoadVar, Data: &runtime.LoadVarData{Name: "Value", Sym: runtime.SymbolRef{Name: "Value", Kind: runtime.SymbolGlobal}}},
						},
					},
				},
			},
		},
	}

	asm := prog.Disassemble()
	expected := []string{
		"; modules    1",
		"section .modules",
		"module.lib: ; path lib hash=hash-lib package=lib",
		"module.lib.global.Value: ; names=Value",
		"DECLARE_INIT_VARS  per_binding bindings=1 values=1 names=Value",
		"PUSH               7",
		"module.lib.fn.Run: ; signature function() Int64",
		"RETURN             1",
		"LOAD_VAR           global[0]=Value",
	}
	assertContainsAll(t, asm, expected)
}

func TestDisassembleDoesNotDuplicateGroupedZeroInitGlobalsInBSS(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Globals: map[string]*runtime.PreparedGlobal{
			"counter": {Name: "counter", Kind: runtime.MustParseRuntimeType("Int64")},
		},
		GlobalInitGroups: []*runtime.PreparedGlobalInit{
			{
				Names: []string{"counter"},
				InitPlan: []runtime.Task{{Op: runtime.OpDeclareInitVars, Data: &runtime.VarDeclData{
					Mode: runtime.VarDeclInitZero,
					Bindings: []runtime.DeclareVarData{
						{Name: "counter", Kind: runtime.MustParseRuntimeType("Int64")},
					},
				}}},
			},
		},
	}

	asm := prog.Disassemble()
	if strings.Contains(asm, "global.counter: resq 1") {
		t.Fatalf("grouped zero init global should not be duplicated in bss:\n%s", asm)
	}
	assertContainsAll(t, asm, []string{
		"global.counter: ; names=counter",
		"DECLARE_INIT_VARS  zero bindings=1 values=0 names=counter",
	})
}

func TestBytecodeJSONRejectsNonCanonicalExecutableType(t *testing.T) {
	payload := []byte(`{
		"format":"go-mini-bytecode",
		"version":7,
		"opcode_set":"runtime.opcode.v4",
		"executable":{
			"global_init_order":[],
			"globals":{
				"counter":{"name":"counter","kind":{"Kind":3,"Raw":"int"},"has_init":false}
			},
			"functions":{},
			"main_tasks":[]
		}
	}`)
	_, err := UnmarshalJSON(payload)
	if err == nil {
		t.Fatal("expected non-canonical executable type rejection")
	}
	if !strings.Contains(err.Error(), "non-canonical type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
