package integrations_test

import (
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler/target"
)

func TestIntegrationCapturedCalls(t *testing.T) {
	engine := newIntegrationEngine(t, "execution/captured_calls", "example", target.Target{})
	for _, tc := range []struct {
		name, function string
		want           int64
	}{
		{"method receiver order", "MethodReceiverOrder", 11},
		{"interface capture", "InterfaceCapture", 13},
		{"go local shadow", "GoLocalShadow", 7},
		{"defer local shadow", "DeferLocalShadow", 7},
		{"defer function value ellipsis", "DeferFunctionValueEllipsis", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program, result, err := engine.Compile("example", minigo.EntryPoint{Name: "result", Function: tc.function})
			if err != nil || !result.OK() {
				t.Fatalf("compile: %v %+v", err, result)
			}
			requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), tc.want)
		})
	}
}

func TestGenericImportsExecuteWithDefinitionScope(t *testing.T) {
	engine := newIntegrationEngine(t, "execution/generic_imports", "example", target.Target{})
	for i := 0; i < 2; i++ {
		program, result, err := engine.Compile("example", minigo.EntryPoint{Name: "result", Function: "Result"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: %v %+v", err, result)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), 29)
	}
}
