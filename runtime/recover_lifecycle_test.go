package runtime

import (
	"context"
	"encoding/json"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestRecoveredPanicSurvivesNestedDeferredCalls(t *testing.T) {
	for _, nestedPanic := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal cleanup", true: "nested recovered panic"}[nestedPanic], func(t *testing.T) {
			artifact := ir.NewArtifact("recover/lifecycle", "main")
			artifact.Constants = []ir.Constant{{ID: "message", Type: testType("String"), Value: json.RawMessage(`"failed"`)}}
			artifact.Functions = []ir.Function{
				{ID: "fn.entry", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{
					{Op: string(ir.OpMakeClosure), Payload: json.RawMessage(`{"function":"recover"}`)},
					{Op: string(ir.OpDeferPush)},
					{Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"message"}`)},
					{Op: string(ir.OpPanic)},
				}},
				{ID: "recover", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{
					{Op: string(ir.OpRecover)},
					{Op: string(ir.OpPop)},
					{Op: string(ir.OpCallDirect), Payload: json.RawMessage(`{"function":"helper","arg_count":0,"result_count":0}`)},
					{Op: string(ir.OpReturn), Payload: json.RawMessage(`{"result_count":0}`)},
				}},
				{ID: "helper", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{
					{Op: string(ir.OpMakeClosure), Payload: json.RawMessage(`{"function":"cleanup"}`)},
					{Op: string(ir.OpDeferPush)},
					{Op: string(ir.OpReturn), Payload: json.RawMessage(`{"result_count":0}`)},
				}},
				{ID: "cleanup", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{
					{Op: string(ir.OpRecover)},
					{Op: string(ir.OpPop)},
					{Op: string(ir.OpReturn), Payload: json.RawMessage(`{"result_count":0}`)},
				}},
			}
			if nestedPanic {
				artifact.Functions[2].Instructions = append(artifact.Functions[2].Instructions[:2],
					ir.Instruction{Op: string(ir.OpConst), Payload: json.RawMessage(`{"constant":"message"}`)},
					ir.Instruction{Op: string(ir.OpPanic)})
			}
			instance, err := patchTestProgram(t, artifact, "recover-lifecycle").Instantiate(context.Background(), InstanceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Call(context.Background(), "run"); err != nil {
				t.Fatalf("nested cleanup lost the recovered outer panic: %v", err)
			}
		})
	}
}
