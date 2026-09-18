package runtime

import (
	"context"
	"errors"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestDebugScopeContainsFinalProgramCounter(t *testing.T) {
	scopes := []ir.DebugScope{{ID: 1, Ranges: []ir.PCRange{{Start: 0, End: 6}}}, {ID: 2, Parent: 1, Ranges: []ir.PCRange{{Start: 2, End: 4}}}}
	if !debugScopeContainsPC(scopes, 2, 2) || !debugScopeContainsPC(scopes, 2, 3) || debugScopeContainsPC(scopes, 2, 4) {
		t.Fatal("nested lexical scope did not use half-open PC ranges")
	}
	if !debugScopeContainsPC(scopes, 0, 100) || debugScopeContainsPC(scopes, 9, 0) {
		t.Fatal("unscoped or unknown local visibility is incorrect")
	}
}

func TestStepDoesNotRequireAnActiveBreakpoint(t *testing.T) {
	artifact := patchCallArtifact(10, 1)
	setTestInstructionLocations(t, &artifact,
		testInstructionLocation{function: "fn.entry", pc: 1, line: 1, column: 1},
		testInstructionLocation{function: "fn.entry", pc: 2, line: 2, column: 1},
	)
	program := patchTestProgram(t, artifact, "step-without-breakpoint")
	instance, err := program.Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if state, _, err := execution.PollSteps(1); err != nil || state != ExecutionRunning {
		t.Fatalf("initial poll = %s, %v", state, err)
	}
	if err := execution.RequestPause(); err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != ExecutionPaused {
		t.Fatalf("pause poll = %s, %v", state, err)
	}
	if _, err := execution.StepInto(); err != nil {
		t.Fatal(err)
	}
	if execution.State() != ExecutionPaused {
		t.Fatalf("StepInto state = %s", execution.State())
	}
	snapshot, err := execution.DebugSnapshot()
	if err != nil || len(snapshot.Frames) == 0 || snapshot.Frames[0].Line != 2 || !snapshot.Frames[0].HasSymbols {
		t.Fatalf("step snapshot = %#v, %v", snapshot, err)
	}
}

func TestCodeOnlyPauseRejectsSourceInspectionAndStepping(t *testing.T) {
	program := patchTestProgram(t, patchCallArtifact(10, 1), "code-only-debug")
	program.symbols = nil
	instance, err := program.Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if state, _, err := execution.PollSteps(1); err != nil || state != ExecutionRunning {
		t.Fatalf("initial poll = %s, %v", state, err)
	}
	if err := execution.RequestPause(); err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != ExecutionPaused {
		t.Fatalf("pause poll = %s, %v", state, err)
	}
	snapshot, err := execution.DebugSnapshot()
	if err != nil || len(snapshot.Frames) == 0 || snapshot.Frames[0].ProgramHash != program.Hash() || snapshot.Frames[0].FunctionID == "" {
		t.Fatalf("code-only snapshot = %#v, %v", snapshot, err)
	}
	if _, err := execution.DebugScopes(snapshot.Frames[0].ID); !errors.Is(err, ErrDebugSymbolsUnavailable) {
		t.Fatalf("DebugScopes error = %v", err)
	}
	if _, err := execution.DebugVariables(1, 0, 0); !errors.Is(err, ErrDebugSymbolsUnavailable) {
		t.Fatalf("DebugVariables error = %v", err)
	}
	steps := []struct {
		name string
		run  func() (RunResult, error)
	}{{"into", execution.StepInto}, {"over", execution.StepOver}, {"out", execution.StepOut}}
	for _, step := range steps {
		if _, err := step.run(); !errors.Is(err, ErrDebugSymbolsUnavailable) {
			t.Fatalf("Step%s error = %v", step.name, err)
		}
		if execution.State() != ExecutionPaused {
			t.Fatalf("Step%s consumed the paused execution", step.name)
		}
	}
	if _, err := execution.Continue(); err != nil {
		t.Fatalf("Continue failed: %v", err)
	}
}
