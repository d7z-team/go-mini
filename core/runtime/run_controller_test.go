package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunControllerResumeRequiresPausedPhase(t *testing.T) {
	controller := NewRunController(NewVMClock())
	defer controller.Stop(context.Canceled)

	if !controller.RequestPause(PauseReason{Kind: "test"}) {
		t.Fatal("expected pause request to succeed")
	}
	if controller.Resume() {
		t.Fatal("resume should be rejected before the VM reaches paused phase")
	}
	if phase := controller.Phase(); phase != RunPhasePausing {
		t.Fatalf("expected pausing phase, got %d", phase)
	}
	if !controller.EnterPaused() {
		t.Fatal("expected controller to enter paused phase")
	}
	if phase := controller.Phase(); phase != RunPhasePaused {
		t.Fatalf("expected paused phase, got %d", phase)
	}
	if !controller.Resume() {
		t.Fatal("resume should succeed after paused phase is reached")
	}
	if phase := controller.Phase(); phase != RunPhaseRunning {
		t.Fatalf("expected running phase after resume, got %d", phase)
	}
}

func TestRunControllerWaitPausedReturnsAfterResume(t *testing.T) {
	controller := NewRunController(NewVMClock())
	defer controller.Stop(context.Canceled)

	if !controller.RequestPause(PauseReason{Kind: "test"}) {
		t.Fatal("expected pause request to succeed")
	}
	done := make(chan error, 1)
	go func() {
		done <- controller.WaitPaused(context.Background())
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if controller.Phase() == RunPhasePaused {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if phase := controller.Phase(); phase != RunPhasePaused {
		t.Fatalf("expected paused phase, got %d", phase)
	}
	if !controller.Resume() {
		t.Fatal("expected resume to succeed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait paused returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for paused wait to resume")
	}
}

func TestRunHandleReportsRuntimePanic(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.TaskStack = append(session.TaskStack, Task{Op: OpDoCall, Data: "bad"})

	run, err := exec.startRun(context.Background(), session, true)
	if err != nil {
		t.Fatalf("startRun failed: %v", err)
	}
	err = run.Wait()
	if err == nil || !strings.Contains(err.Error(), "runtime panic") {
		t.Fatalf("expected runtime panic error, got %v", err)
	}
}
