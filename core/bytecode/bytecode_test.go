package bytecode

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/runtime"
)

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
			"counter": {Name: "counter", Kind: runtime.MustParseRuntimeType("Int64"), HasInit: true},
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
		"global.counter: ; has_init=true",
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
	for _, sym := range expected {
		if !strings.Contains(asm, sym) {
			t.Fatalf("expected %q in disassembly, got:\n%s", sym, asm)
		}
	}
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
	for _, sym := range expected {
		if !strings.Contains(asm, sym) {
			t.Fatalf("expected %q in disassembly, got:\n%s", sym, asm)
		}
	}
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
