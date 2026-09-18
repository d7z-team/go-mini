package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestLibraryBackgroundTaskCanBeInspectedAndContinued(t *testing.T) {
	child := delayedLifecycleChild(ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})})
	artifact := lifecycleArtifact(child)
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.child", pc: len(child) - 1, line: 10, column: 2})
	program := patchTestProgram(t, artifact, "library-background-debug")
	instance, err := program.Instantiate(context.Background(), InstanceOptions{Debugger: NewDebugger()})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	resolved, err := instance.SetBreakpoints(artifact.Module.Path, "main.mgo", []int{10})
	if err != nil || len(resolved) != 1 || !resolved[0].Verified {
		t.Fatalf("background breakpoint = %#v, %v", resolved, err)
	}
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-execution.Updates():
	case <-time.After(time.Second):
		t.Fatal("background breakpoint did not publish an update")
	}
	snapshot, err := execution.DebugSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Threads) == 0 || len(snapshot.Frames) == 0 || snapshot.Frames[0].FunctionID != "fn.child" {
		t.Fatalf("background debug snapshot = %#v", snapshot)
	}
	if _, err := execution.Continue(); err != nil {
		t.Fatal(err)
	}
	if execution.State() != ExecutionCompleted || !instance.isOpen() {
		t.Fatalf("continued background task changed invocation state: execution=%s instance=%d", execution.State(), instance.lifecycleState())
	}
	if scope, err := execution.WaitScope(context.Background()); err != nil || !scope.Done {
		t.Fatalf("continued background scope = %#v, %v", scope, err)
	}
	canceled, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canceled.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled.Updates():
	case <-time.After(time.Second):
		t.Fatal("second background breakpoint did not publish an update")
	}
	if _, err := canceled.DebugSnapshot(); err != nil {
		t.Fatal(err)
	}
	canceled.Cancel()
	if _, err := canceled.WaitScope(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled paused scope = %v", err)
	}
	if _, err := canceled.DebugSnapshot(); err == nil {
		t.Fatal("canceled scope retained its debug inspection")
	}
}
