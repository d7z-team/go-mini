package bytecode

import "testing"

func FuzzCallIntrinsicPayload(f *testing.F) {
	f.Add("reflect.type_of", int8(1), int8(1), false)
	f.Add("reflect.value_call", int8(3), int8(3), false)
	f.Add("reflect.value_set", int8(2), int8(2), false)
	f.Add("reflect.unknown", int8(0), int8(0), false)
	f.Add("reflect.type_of", int8(1), int8(1), true)

	f.Fuzz(func(t *testing.T, id string, argInput, resultInput int8, extra bool) {
		if len(id) > 256 {
			t.Skip()
		}
		argCount, resultCount := int(argInput), int(resultInput)
		payload := map[string]any{
			"id": id, "arg_count": argCount, "result_count": resultCount,
		}
		if extra {
			payload["unknown"] = true
		}
		instruction := Instruction{Op: string(OpCallIntrinsic), Payload: testPayload(payload)}
		err := validateInstruction("functions[0].instructions[0]", instruction)
		descriptor, known := Intrinsic(IntrinsicID(id))
		valid := known && !extra && argCount == descriptor.ArgCount && resultCount == descriptor.ResultCount
		if !valid {
			if err == nil {
				t.Fatalf("invalid intrinsic payload was accepted: %s", instruction.Payload)
			}
			return
		}
		if err != nil {
			t.Fatalf("valid intrinsic payload was rejected: %v", err)
		}
		need, delta, terminal, err := instructionStackEffect(instruction)
		if err != nil || need != descriptor.ArgCount || delta != descriptor.ResultCount-descriptor.ArgCount || terminal {
			t.Fatalf("intrinsic stack effect = need %d delta %d terminal %t err %v", need, delta, terminal, err)
		}
	})
}
