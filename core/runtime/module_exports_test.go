package runtime

import (
	"strings"
	"testing"
)

func TestModuleMemberAccessUsesExplicitDataOnly(t *testing.T) {
	exec, err := NewExecutorFromPrepared(&PreparedProgram{
		Globals:   map[string]*PreparedGlobal{},
		Functions: map[string]*PreparedFunction{},
		MainTasks: []Task{},
	})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	shared := NewSharedState()
	shared.StoreGlobal("hidden", NewInt(99))
	mod := &VMModule{
		Name: "demo",
		Data: map[string]*Var{
			"Value": NewInt(1),
		},
		Context: &LexicalContext{
			Executor: exec,
			Shared:   shared,
			Stack:    &Stack{},
		},
	}
	moduleValue := &Var{VType: TypeModule, TypeInfo: MustParseRuntimeType(SpecModule), Ref: mod}

	got, err := exec.evalMemberExprDirect(nil, moduleValue, "Value")
	if err != nil {
		t.Fatalf("load exported member: %v", err)
	}
	if got == nil || got.VType != TypeInt || got.I64 != 1 {
		t.Fatalf("unexpected exported value: %#v", got)
	}
	if _, err := exec.evalMemberExprDirect(nil, moduleValue, "hidden"); err == nil || !strings.Contains(err.Error(), "module member hidden not found") {
		t.Fatalf("expected hidden member rejection, got %v", err)
	}
	if got, ok := exec.resolveMethodValue(moduleValue, "hidden"); ok || got != nil {
		t.Fatalf("lookupMember must not resolve hidden context value: %#v", got)
	}
}
