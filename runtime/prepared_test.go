package runtime

import (
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestPreparedInstructionDecodesOperator(t *testing.T) {
	instruction, err := prepareInstruction(ir.Instruction{Op: string(ir.OpBinary), Payload: []byte(`{"operator":"+"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if instruction.op != preparedBinary || instruction.operator != operatorAdd {
		t.Fatalf("prepared instruction = %#v", instruction)
	}
	if instruction.control {
		t.Fatal("ordinary binary instruction was classified as scheduler control")
	}
}

func TestPreparedInstructionBindsRuntimeTypeAndStructSchema(t *testing.T) {
	table := &types.TypeTable{}
	ref, err := types.NewParser("example/module", table).Parse("struct{Value:Int}")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(ir.MakeStructPayload{Type: ref, Fields: []string{"Value"}})
	if err != nil {
		t.Fatal(err)
	}
	instruction, err := prepareInstruction(ir.Instruction{Op: string(ir.OpMakeStruct), Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	instruction.bindTypes(table, "example/module")
	if got := instruction.runtimeType.String(); got != "struct{Value:Int}" {
		t.Fatalf("prepared type = %q", got)
	}
	if instruction.structSchema == nil || len(instruction.structSchema.fields) != 1 || instruction.structSchema.fields[0].Name != "Value" {
		t.Fatalf("prepared schema = %#v", instruction.structSchema)
	}
}

func TestPreparedDirectCallUsesCurrentModuleByDefault(t *testing.T) {
	payload, err := json.Marshal(ir.CallPayload{Function: "fn.target"})
	if err != nil {
		t.Fatal(err)
	}
	instruction, err := prepareInstruction(ir.Instruction{Op: string(ir.OpCallDirect), Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	instruction.bindTypes(&types.TypeTable{}, "example/module")
	if instruction.callModule != "example/module" {
		t.Fatalf("prepared call module = %q", instruction.callModule)
	}
	if !instruction.control {
		t.Fatal("direct call was not classified as scheduler control")
	}
}

func TestPreparedTailCallIsSchedulerControl(t *testing.T) {
	payload, err := json.Marshal(ir.CallPayload{Function: "fn.target", ResultCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	instruction, err := prepareInstruction(ir.Instruction{Op: string(ir.OpTailCallDirect), Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if instruction.op != preparedTailCallDirect || !instruction.control {
		t.Fatalf("prepared tail call = %#v", instruction)
	}
}
