package integrations_test

import (
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler/target"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func TestIntegrationSources(t *testing.T) {
	t.Run("diagnostic", func(t *testing.T) {
		engine := newIntegrationEngine(t, "compiler/diagnostic", "integration/diagnostic", target.Target{})
		result, err := engine.Check("integration/diagnostic")
		if err != nil || result.OK() || len(result.Diagnostics) == 0 {
			t.Fatalf("check: result=%#v err=%v", result, err)
		}
	})

	t.Run("build tags", func(t *testing.T) {
		engine := newIntegrationEngine(t, "compiler/tags", "integration/tags", target.Target{Tags: []string{"feature"}})
		program, result, err := engine.Compile("integration/tags", minigo.EntryPoint{Name: "answer", Function: "Answer"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "answer"), 42)
	})

	t.Run("compiler output", func(t *testing.T) {
		engine := newIntegrationEngine(t, "compiler/simple", "integration/simple", target.Target{})
		program, result, err := engine.Compile("integration/simple",
			minigo.EntryPoint{Name: "answer", Function: "Answer"},
			minigo.EntryPoint{Name: "add", Function: "Add"},
		)
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "answer"), 42)
		requireIntegrationInt(t, callIntegrationEntry(t, program, "add", minigoruntime.HostInt("Int", 20), minigoruntime.HostInt("Int", 22)), 42)
	})

	t.Run("local shadowing", func(t *testing.T) {
		engine := newIntegrationEngine(t, "compiler/shadow", "integration/shadow", target.Target{})
		program, result, err := engine.Compile("integration/shadow", minigo.EntryPoint{Name: "result", Function: "Result"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), 42)
	})

	t.Run("runtime values", func(t *testing.T) {
		engine := newIntegrationEngine(t, "execution/runtime", "integration/runtime", target.Target{})
		program, result, err := engine.Compile("integration/runtime",
			minigo.EntryPoint{Name: "answer", Function: "Answer"},
			minigo.EntryPoint{Name: "array-slice", Function: "ArraySliceMutation"},
		)
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "answer"), 42)
		requireIntegrationInt(t, callIntegrationEntry(t, program, "array-slice"), 9886)
	})

	t.Run("array storage", func(t *testing.T) {
		engine := newIntegrationEngine(t, "execution/arrays", "integration/arrays", target.Target{})
		program, result, err := engine.Compile("integration/arrays", minigo.EntryPoint{Name: "storage", Function: "StorageMutation"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		value := callIntegrationEntry(t, program, "storage")
		got, ok := value.Uint64()
		if !ok || got != 16908292 {
			t.Fatalf("storage result = %#v, want 16908292", value)
		}
	})

	t.Run("deferred recover", func(t *testing.T) {
		engine := newIntegrationEngine(t, "execution/recover", "integration/recover", target.Target{})
		program, result, err := engine.Compile("integration/recover", minigo.EntryPoint{Name: "result", Function: "Result"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), 7)
	})

	t.Run("overloaded compound target", func(t *testing.T) {
		engine := newIntegrationEngine(t, "execution/operators", "integration/operators", target.Target{})
		program, result, err := engine.Compile("integration/operators", minigo.EntryPoint{Name: "result", Function: "Result"})
		if err != nil || !result.OK() {
			t.Fatalf("compile: result=%#v err=%v", result, err)
		}
		requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), 421)
	})
}
