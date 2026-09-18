package runtime

import (
	"context"
	"sync"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestPatchResolvesRequestedBreakpointsAgainstNewRevision(t *testing.T) {
	artifactAtLine := func(delta, line int64) ir.Artifact {
		artifact := patchGlobalArtifact(delta)
		setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.entry", pc: 0, line: int(line), column: 1})
		return artifact
	}
	debugger := NewDebugger()
	instance, err := patchTestProgram(t, artifactAtLine(1, 4), "breakpoint-old").Instantiate(context.Background(), InstanceOptions{Debugger: debugger})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	resolved, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{4})
	if err != nil || len(resolved) != 1 || resolved[0].Line != 4 {
		t.Fatalf("initial breakpoints = %#v, %v", resolved, err)
	}
	plan, err := instance.PreparePatch(context.Background(), patchTestProgram(t, artifactAtLine(2, 8), "breakpoint-new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 4) {
		t.Fatal("patch retained the old resolved line")
	}
	if !debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 8) {
		t.Fatal("patch did not resolve the requested line against the new revision")
	}
	if debugger.hasBreakpoint(1, "patch/global", "main.mgo", 8) {
		t.Fatal("new revision breakpoint remained active for the retired revision")
	}
}

func TestConcurrentBreakpointUpdateCommitsCurrentRevisionMapping(t *testing.T) {
	artifactAtLine := func(delta int64, line int) ir.Artifact {
		artifact := patchGlobalArtifact(delta)
		setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.entry", pc: 0, line: line, column: 1})
		return artifact
	}
	instance, err := patchTestProgram(t, artifactAtLine(1, 4), "breakpoint-race-old").Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	plan, err := instance.PreparePatch(context.Background(), patchTestProgram(t, artifactAtLine(2, 8), "breakpoint-race-new"))
	if err != nil {
		t.Fatal(err)
	}

	instance.vm.debugger.mu.Lock()
	var group sync.WaitGroup
	group.Add(2)
	applyResult := make(chan error, 1)
	breakpointResult := make(chan error, 1)
	go func() {
		defer group.Done()
		_, applyErr := instance.ApplyPatch(plan)
		applyResult <- applyErr
	}()
	go func() {
		defer group.Done()
		_, breakpointErr := instance.SetBreakpoints("patch/global", "main.mgo", []int{4})
		breakpointResult <- breakpointErr
	}()
	instance.vm.debugger.mu.Unlock()
	group.Wait()
	if err := <-applyResult; err != nil {
		t.Fatal(err)
	}
	if err := <-breakpointResult; err != nil {
		t.Fatal(err)
	}

	generation := instance.Revision().Generation
	if generation != 2 || !instance.vm.debugger.hasBreakpoint(generation, "patch/global", "main.mgo", 8) ||
		instance.vm.debugger.hasBreakpoint(generation, "patch/global", "main.mgo", 4) {
		t.Fatalf("concurrent breakpoint mapping was not committed for generation %d", generation)
	}
}
