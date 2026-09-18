package runtime

import (
	"context"
	"testing"
)

func TestBreakpointUpdateWaitsForCloseBoundary(t *testing.T) {
	instance, err := patchTestProgram(t, patchGlobalArtifact(1), "close-breakpoint").Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	debugger := instance.vm.debugger
	debugger.mu.Lock()
	updated := make(chan error, 1)
	go func() {
		_, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{1})
		updated <- err
	}()
	// Shutdown publishes Closing synchronously, even when its cleanup cannot yet
	// acquire the debugger lock. Neither lock ordering depends on a sleep.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = instance.Shutdown(ctx)
	debugger.mu.Unlock()
	if err := <-updated; err == nil {
		t.Fatal("breakpoint update succeeded after Closing")
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if instance.vm.debugger != debugger || !debugger.closed || len(debugger.breakpoints) != 0 || debugger.paused != nil {
		t.Fatal("closed debugger retained guest state or lost its stable identity")
	}
	if len(debugger.Events()) != 0 {
		t.Fatal("closed debugger retained events")
	}
}

func TestBreakpointAndPatchShareRevisionBoundary(t *testing.T) {
	old := patchGlobalArtifact(1)
	updated := patchGlobalArtifact(2)
	instance, err := patchTestProgram(t, old, "breakpoint-old").Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	plan, err := instance.PreparePatch(context.Background(), patchTestProgram(t, updated, "breakpoint-new"))
	if err != nil {
		t.Fatal(err)
	}
	debugger := instance.vm.debugger
	debugger.mu.Lock()
	patched, breakpoint := make(chan error, 1), make(chan error, 1)
	go func() { _, err := instance.ApplyPatch(plan); patched <- err }()
	go func() { _, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{1}); breakpoint <- err }()
	debugger.mu.Unlock()
	if err := <-patched; err != nil {
		t.Fatal(err)
	}
	if err := <-breakpoint; err != nil {
		t.Fatal(err)
	}
	debugger.mu.Lock()
	defer debugger.mu.Unlock()
	for _, set := range debugger.breakpoints {
		if set.generation != instance.vm.revision.Load().generation {
			t.Fatal("breakpoint committed against a retired revision")
		}
	}
}

func TestDebuggerCannotBeSharedOrRebound(t *testing.T) {
	program := patchTestProgram(t, patchGlobalArtifact(1), "debugger-owner")
	debugger := NewDebugger()
	instance, err := program.Instantiate(context.Background(), InstanceOptions{Debugger: debugger})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	if other, err := program.Instantiate(context.Background(), InstanceOptions{Debugger: debugger}); err == nil {
		_ = other.Close()
		t.Fatal("shared debugger accepted")
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if other, err := program.Instantiate(context.Background(), InstanceOptions{Debugger: debugger}); err == nil {
		_ = other.Close()
		t.Fatal("closed debugger rebound")
	}
}
