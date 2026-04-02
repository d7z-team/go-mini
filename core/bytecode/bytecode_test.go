package bytecode

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
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

func TestRebuildProgramFromBlueprintAndExecutable(t *testing.T) {
	prog := NewProgram()
	prog.Blueprint = &Blueprint{
		Package:   "demo",
		Constants: map[string]string{"Version": "v1"},
		Types:     map[ast.Ident]ast.GoMiniType{"Alias": "String"},
		Structs: map[ast.Ident]*ast.StructStmt{
			"Payload": {
				Name:       "Payload",
				Fields:     map[ast.Ident]ast.GoMiniType{"Msg": "String"},
				FieldNames: []ast.Ident{"Msg"},
			},
		},
	}
	prog.Executable = &runtime.PreparedProgram{
		Globals: map[ast.Ident]*runtime.PreparedGlobal{
			"counter": {Name: "counter", HasInit: true},
		},
		Functions: map[ast.Ident]*runtime.PreparedFunction{
			"main": {Name: "main"},
		},
	}

	rebuilt, err := prog.RebuildProgram()
	if err != nil {
		t.Fatalf("rebuild program failed: %v", err)
	}
	if rebuilt.Package != "demo" {
		t.Fatalf("unexpected package: %s", rebuilt.Package)
	}
	if rebuilt.Constants["Version"] != "v1" {
		t.Fatalf("unexpected constants: %#v", rebuilt.Constants)
	}
	if rebuilt.Structs["Payload"] == nil {
		t.Fatalf("expected struct metadata: %#v", rebuilt.Structs)
	}
	if _, ok := rebuilt.Variables["counter"]; !ok {
		t.Fatalf("expected global names from executable: %#v", rebuilt.Variables)
	}
	if rebuilt.Functions["main"] == nil {
		t.Fatalf("expected function stubs from executable: %#v", rebuilt.Functions)
	}
}
