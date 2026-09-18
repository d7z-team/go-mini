package runtime

import (
	"encoding/json"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestLoaderUsesValidatedStackBound(t *testing.T) {
	artifact := ir.NewArtifact("test/stack", "stack")
	artifact.Functions = []ir.Function{{
		ID: "fn.value", Signature: testSignature("function() Bool"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
		},
	}}
	attachRuntimeTestTypeNodes(&artifact)
	for _, claimed := range []int{0, 1, 2} {
		artifact.Functions[0].MaxStack = claimed
		data, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		for _, jsonInput := range []bool{false, true} {
			var loaded *executable
			if jsonInput {
				loaded, err = newLoader().loadJSON(data)
			} else {
				loaded, err = newLoader().load(artifact)
			}
			if claimed == 2 {
				if err == nil {
					t.Fatal("inconsistent stack bound accepted")
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if fn := loaded.Functions["fn.value"]; fn.MaxStack != 1 || fn.Decl.MaxStack != 1 {
				t.Fatalf("loaded stack bound: prepared=%d declaration=%d", fn.MaxStack, fn.Decl.MaxStack)
			}
		}
	}
}

func TestLoaderRemovesLabelsAndRelocatesJumps(t *testing.T) {
	artifact := ir.NewArtifact("test/labels", "labels")
	artifact.Functions = []ir.Function{{
		ID:        "fn.main",
		Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpLabel), Payload: testPayload(ir.LabelPayload{Label: "entry"})},
			{Op: string(ir.OpJump), Payload: testPayload(ir.JumpPayload{Label: "exit"})},
			{Op: string(ir.OpLabel), Payload: testPayload(ir.LabelPayload{Label: "exit"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
		},
	}}
	attachRuntimeTestTypeNodes(&artifact)

	executable, err := newLoader().load(artifact)
	if err != nil {
		t.Fatalf("load artifact: %v", err)
	}
	function := executable.Functions["fn.main"]
	if len(function.Instructions) != 2 {
		t.Fatalf("prepared instruction count = %d, want 2", len(function.Instructions))
	}
	if function.Instructions[0].op != preparedJump || function.Instructions[0].jumpPC != 1 {
		t.Fatalf("prepared jump = %#v, want target pc 1", function.Instructions[0])
	}
}
