package bytecode

import (
	"encoding/json"
	"strconv"
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

func assertContainsInOrder(t *testing.T, text string, expected []string) {
	t.Helper()
	offset := 0
	for _, sym := range expected {
		idx := strings.Index(text[offset:], sym)
		if idx < 0 {
			t.Fatalf("expected %q after offset %d in output, got:\n%s", sym, offset, text)
		}
		offset += idx + len(sym)
	}
}

func assertNotContainsAll(t *testing.T, text string, unexpected []string) {
	t.Helper()
	for _, sym := range unexpected {
		if strings.Contains(text, sym) {
			t.Fatalf("did not expect %q in output, got:\n%s", sym, text)
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

func TestRefreshDisplayKeepsEmptyInstructionListsAsArrays(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{}
	prog.RefreshDisplayFromExecutable()

	payload, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for _, key := range []string{"globals", "entry", "functions"} {
		if string(raw[key]) != "[]" {
			t.Fatalf("top-level %s = %s, want []", key, raw[key])
		}
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

func TestDisassembleUsesPreparedExecutionOrder(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
				BodyTasks: []runtime.Task{
					{Op: runtime.OpScopeExit},
					{Op: runtime.OpPush, Data: runtime.NewInt(1)},
					{Op: runtime.OpScopeEnter, Data: "block"},
				},
			},
		},
	}

	prog.RefreshDisplayFromExecutable()
	if len(prog.Functions) != 1 {
		t.Fatalf("unexpected function preview: %+v", prog.Functions)
	}
	instructions := prog.Functions[0].Instructions
	if len(instructions) != 3 {
		t.Fatalf("unexpected instruction preview: %+v", instructions)
	}
	expectedOps := []string{
		runtime.OpScopeEnter.String(),
		runtime.OpPush.String(),
		runtime.OpScopeExit.String(),
	}
	for idx, op := range expectedOps {
		if instructions[idx].Op != op {
			t.Fatalf("instruction %d = %s, want %s; preview=%+v", idx, instructions[idx].Op, op, instructions)
		}
	}

	assertContainsInOrder(t, prog.Disassemble(), []string{
		"SCOPE_ENTER        block",
		"PUSH               1",
		"SCOPE_EXIT         block",
	})
}

func TestDisassembleHidesInternalNoopTasks(t *testing.T) {
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
				BodyTasks: []runtime.Task{
					{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{Kind: runtime.LHSTypeEnv, Name: "x", Sym: runtime.SymbolRef{Name: "x", Kind: runtime.SymbolLocal}}},
					{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{Kind: runtime.LHSTypeNone}},
					{Op: runtime.OpLineStep},
					{Op: runtime.OpPush, Data: runtime.NewInt(1)},
				},
			},
		},
	}

	prog.RefreshDisplayFromExecutable()
	instructions := prog.Functions[0].Instructions
	if len(instructions) != 2 {
		t.Fatalf("unexpected visible instructions: %+v", instructions)
	}
	if instructions[0].Op != runtime.OpPush.String() || instructions[1].Operand != "env x local[0]=x" {
		t.Fatalf("unexpected instruction preview: %+v", instructions)
	}

	asm := prog.Disassemble()
	assertContainsAll(t, asm, []string{
		"PUSH               1",
		"EVAL_LHS           env x local[0]=x",
	})
	assertNotContainsAll(t, asm, []string{
		"EVAL_LHS           none",
		"LINE_STEP",
	})
}

func TestDisassembleFullyExpandsPreparedSwitchBlocks(t *testing.T) {
	switchData := &runtime.SwitchData{
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
	}
	prog := NewProgram()
	prog.Executable = &runtime.PreparedProgram{
		Package: "demo",
		Functions: map[string]*runtime.PreparedFunction{
			"main": {
				Name:        "main",
				FunctionSig: runtime.MustParseRuntimeFuncSig("function() Void"),
				BodyTasks: []runtime.Task{
					{Op: runtime.OpSwitchStart, Data: switchData},
					switchData.Tag[0],
					switchData.Init[0],
				},
			},
		},
	}

	asm := prog.Disassemble()
	expected := []string{
		"fn.main: ; signature function() Void",
		"PUSH               1",
		"LOAD_VAR           v",
		"SWITCH_START",
		"case_0_match_0->fn.main.L0000.case_0_match_0",
		"case_0_match_1->fn.main.L0001.case_0_match_1",
		"case_0->fn.main.L0002.case_0",
		"default->fn.main.L0003.default",
		"fn.main.L0000.case_0_match_0:",
		"PUSH               2",
		"fn.main.L0001.case_0_match_1:",
		"PUSH               3",
		"fn.main.L0002.case_0:",
		"RETURN",
		"fn.main.L0003.default:",
		"POP",
	}
	assertContainsInOrder(t, asm, expected)
	assertNotContainsAll(t, asm, []string{
		".init:",
		".tag:",
	})
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

func TestBytecodeRejectsEmbeddedSourceModules(t *testing.T) {
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
				Package:   "lib",
				Globals:   map[string]*runtime.PreparedGlobal{},
				Functions: map[string]*runtime.PreparedFunction{},
			},
		},
	}

	if err := prog.Validate(); err == nil || !strings.Contains(err.Error(), "must not embed source modules") {
		t.Fatalf("expected embedded module validation error, got %v", err)
	}
	if _, err := json.Marshal(prog); err == nil || !strings.Contains(err.Error(), "must not embed source modules") {
		t.Fatalf("expected embedded module marshal error, got %v", err)
	}

	payload := []byte(`{"format":"go-mini-bytecode","version":` + strconv.Itoa(CurrentVersion) + `,"opcode_set":"runtime.opcode.v5","globals":[],"entry":[],"functions":[],"executable":{"modules":{"lib":{}},"global_init_order":[],"globals":{},"functions":{},"main_tasks":[]}}`)
	if _, err := UnmarshalJSON(payload); err == nil || !strings.Contains(err.Error(), "must not embed source modules") {
		t.Fatalf("expected embedded module unmarshal error, got %v", err)
	}
	var decoded Program
	if err := json.Unmarshal(payload, &decoded); err == nil || !strings.Contains(err.Error(), "must not embed source modules") {
		t.Fatalf("expected direct embedded module unmarshal error, got %v", err)
	}

	hashPayload := []byte(`{"format":"go-mini-bytecode","version":` + strconv.Itoa(CurrentVersion) + `,"opcode_set":"runtime.opcode.v5","globals":[],"entry":[],"functions":[],"executable":{"module_hashes":{"lib":"hash-lib"},"global_init_order":[],"globals":{},"functions":{},"main_tasks":[]}}`)
	if _, err := UnmarshalJSON(hashPayload); err == nil || !strings.Contains(err.Error(), "must not embed source module hashes") {
		t.Fatalf("expected embedded module hash unmarshal error, got %v", err)
	}
	decoded = Program{}
	if err := json.Unmarshal(hashPayload, &decoded); err == nil || !strings.Contains(err.Error(), "must not embed source module hashes") {
		t.Fatalf("expected direct embedded module hash unmarshal error, got %v", err)
	}
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
		"version":9,
		"opcode_set":"runtime.opcode.v5",
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
