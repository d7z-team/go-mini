package runtime

import (
	"context"
	"testing"
)

func TestExecutorHotPathUsesPreparedProgramCaches(t *testing.T) {
	exec, err := NewExecutorFromPrepared(&PreparedProgram{
		Constants:       map[string]FFIConstValue{"Answer": ConstInt64(42)},
		ConstantTypes:   map[string]RuntimeType{"Answer": MustParseRuntimeType(SpecInt64)},
		GlobalInitOrder: []string{"counter"},
		Globals: map[string]*PreparedGlobal{
			"counter": {
				Name:     "counter",
				Kind:     MustParseRuntimeType(SpecInt64),
				HasInit:  true,
				InitPlan: []Task{{Op: OpPush, Data: NewInt(2)}},
			},
		},
		Functions: map[string]*PreparedFunction{
			"inc": {
				Name:        "inc",
				FunctionSig: MustParseRuntimeFuncSig("function(Int64) Int64"),
				BodyTasks: []Task{
					{Op: OpReturn, Data: 1},
					{Op: OpApplyBinary, Data: "+"},
					{Op: OpPush, Data: NewInt(1)},
					{Op: OpLoadLocal, Data: SymbolRef{Name: "n", Kind: SymbolLocal, Slot: 0}},
				},
			},
		},
		MainTasks: []Task{},
	})
	if err != nil {
		t.Fatalf("new executor from prepared failed: %v", err)
	}
	session := exec.NewSession(context.Background(), "global")
	if err := exec.InitializeSession(session, nil, false); err != nil {
		t.Fatalf("initialize session failed: %v", err)
	}

	counter, err := session.Load("counter")
	if err != nil {
		t.Fatalf("load counter failed: %v", err)
	}
	if counter.I64 != 2 {
		t.Fatalf("unexpected counter value: %#v", counter)
	}

	result, err := exec.runTemporaryTasks(session, []Task{
		{Op: OpCall, Data: &CallData{Mode: CallByName, Name: "inc", ArgCount: 1}},
		{Op: OpPush, Data: NewInt(41)},
	})
	if err != nil {
		t.Fatalf("exec call expr failed: %v", err)
	}
	if result == nil || result.VType != TypeInt || result.I64 != 42 {
		t.Fatalf("unexpected function result: %#v", result)
	}

	if got := exec.consts["Answer"].DisplayString(); got != "42" {
		t.Fatalf("prepared constants were not cached: %#v", exec.consts)
	}
}
