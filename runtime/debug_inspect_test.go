package runtime

import (
	"context"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestInstanceSetBreakpointsReplacesSourceAtomically(t *testing.T) {
	artifact := patchGlobalArtifact(1)
	locations := []ir.Location{
		{File: "other.mgo", Line: 4, Column: 1},
		{File: "main.mgo", Line: 2, Column: 1},
		{File: "main.mgo", Line: 7, Column: 1},
		{File: "main.mgo", Line: 9, Column: 1},
		{File: "main.mgo", Line: 11, Column: 1},
		{File: "main.mgo", Line: 13, Column: 1},
	}
	for index, location := range locations {
		setTestInstructionLocations(t, &artifact, testInstructionLocation{
			function: "fn.entry", pc: index, file: location.File, line: location.Line, column: location.Column,
		})
	}
	debugger := NewDebugger()
	instance, err := patchTestProgram(t, artifact, "breakpoint-state").Instantiate(context.Background(), InstanceOptions{Debugger: debugger})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	if _, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{2}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.SetBreakpoints("patch/global", "other.mgo", []int{4}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{7, 9}); err != nil {
		t.Fatal(err)
	}
	if debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 2) {
		t.Fatal("replaced breakpoint remains active")
	}
	for _, line := range []int{7, 9} {
		if !debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", line) {
			t.Fatalf("missing breakpoint at line %d", line)
		}
	}
	if !debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "other.mgo", 4) {
		t.Fatal("breakpoint in another source was removed")
	}
}
