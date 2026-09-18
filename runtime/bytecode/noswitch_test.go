package bytecode

import (
	"strings"
	"testing"
)

func TestValidateNoSwitchFunctionInstructions(t *testing.T) {
	valid := NewArtifact("example/noswitch", "main")
	valid.Functions = []Function{{
		ID: "fn.update", NoSwitch: true,
		Signature: testSignature("function() Void"),
		Instructions: []Instruction{
			{Op: string(OpZero), Payload: testPayload(TypePayload{Type: testType("Int")})},
			{Op: string(OpPop)},
			{Op: string(OpReturn), Payload: testPayload(ReturnPayload{})},
		},
	}}
	attachTestTypeNodes(&valid)
	if err := ValidateArtifact(&valid); err != nil {
		t.Fatalf("valid no-switch function: %v", err)
	}
	disassembly, err := Disassemble(&valid)
	if err != nil || !strings.Contains(disassembly, "[no-switch]") {
		t.Fatalf("no-switch disassembly = %q, %v", disassembly, err)
	}

	for _, instruction := range []Instruction{
		{Op: string(OpCallDirect), Payload: testPayload(CallPayload{Function: "fn.update"})},
		{Op: string(OpTailCallDirect), Payload: testPayload(CallPayload{Function: "fn.update"})},
		{Op: string(OpWaitableRecv)},
		{Op: string(OpWaitableSend)},
		{Op: string(OpSpawn), Payload: testPayload(CallPayload{})},
		{Op: string(OpCallFFI), Payload: testPayload(CallFFIPayload{ArgCount: 2, ResultCount: 3})},
	} {
		artifact := NewArtifact("example/noswitch", "main")
		artifact.Functions = []Function{{
			ID: "fn.update", NoSwitch: true,
			Signature:    testSignature("function() Void"),
			Instructions: []Instruction{instruction},
		}}
		attachTestTypeNodes(&artifact)
		err := ValidateArtifact(&artifact)
		if ValidationCode(err) != ValidationNoSwitchInvalid {
			t.Fatalf("opcode %s validation = %v", instruction.Op, err)
		}
	}
}

func FuzzNoSwitchInstructionValidation(f *testing.F) {
	forbidden := map[Opcode]bool{
		OpCallDirect: true, OpTailCallDirect: true, OpCallValue: true, OpCallInterface: true, OpCallFFI: true, OpCallIntrinsic: true,
		OpSpawn: true, OpDeferPush: true, OpInitModule: true, OpLoadExport: true,
		OpWaitableSend: true, OpWaitableRecv: true, OpWaitableRecvOK: true, OpWaitSetPark: true,
	}
	f.Add(string(OpZero))
	for opcode := range forbidden {
		f.Add(string(opcode))
	}
	f.Fuzz(func(t *testing.T, opcode string) {
		if len(opcode) > 128 {
			t.Skip()
		}
		fn := Function{ID: "fn.region", NoSwitch: true, Instructions: []Instruction{{Op: opcode}}}
		err := validateNoSwitchFunction("functions[0]", fn)
		blocked := forbidden[Opcode(opcode)]
		if blocked && ValidationCode(err) != ValidationNoSwitchInvalid {
			t.Fatalf("forbidden opcode %q validation = %v", opcode, err)
		}
		if !blocked && err != nil {
			t.Fatalf("local opcode %q no-switch validation = %v", opcode, err)
		}
	})
}
