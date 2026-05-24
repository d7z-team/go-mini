package debugger

import (
	"sync"
	"testing"
	"time"
)

func TestSessionBreakpointAndSteppingState(t *testing.T) {
	session := NewSession()
	if session.ShouldTrigger(12) {
		t.Fatal("empty session should not trigger")
	}

	session.AddBreakpoint(12)
	if !session.HasBreakpoint(12) || !session.ShouldTrigger(12) {
		t.Fatal("expected breakpoint to trigger")
	}

	session.RemoveBreakpoint(12)
	if session.HasBreakpoint(12) || session.ShouldTrigger(12) {
		t.Fatal("removed breakpoint should not trigger")
	}

	session.SetStepping(true)
	if !session.IsStepping() || !session.ShouldTrigger(99) {
		t.Fatal("stepping should trigger independent of line")
	}
	session.SetStepping(false)
	if session.IsStepping() || session.ShouldTrigger(99) {
		t.Fatal("disabled stepping should not trigger")
	}
}

func TestSessionRequestPauseIsConsumedAtNextSafePoint(t *testing.T) {
	session := NewSession()
	session.RequestPause()
	if !session.ShouldTrigger(1) {
		t.Fatal("expected requested pause to trigger")
	}
	if session.ShouldTrigger(1) {
		t.Fatal("requested pause should be consumed after one trigger")
	}
}

func TestSessionPauseCommandMethods(t *testing.T) {
	session := NewSession()
	done := make(chan Command, 1)
	go func() {
		done <- session.Pause(&Event{ExecutionContextID: 1, Loc: &Position{L: 7}})
	}()

	select {
	case event := <-session.Events():
		if event.Loc == nil || event.Loc.L != 7 {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for debugger event")
	}

	session.StepInto()
	select {
	case cmd := <-done:
		if cmd != CmdStepInto {
			t.Fatalf("unexpected command: %s", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for pause to resume")
	}
}

func TestSessionBreakpointRemoveDoesNotCancelCurrentPause(t *testing.T) {
	session := NewSession()
	session.AddBreakpoint(7)
	if !session.ShouldTrigger(7) {
		t.Fatal("expected breakpoint hit before remove")
	}

	done := make(chan Command, 1)
	go func() {
		done <- session.Pause(&Event{ExecutionContextID: 1, Loc: &Position{L: 7}})
	}()

	select {
	case event := <-session.Events():
		if event.Loc == nil || event.Loc.L != 7 {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for current pause")
	}

	session.RemoveBreakpoint(7)
	session.Continue()
	select {
	case cmd := <-done:
		if cmd != CmdContinue {
			t.Fatalf("unexpected command: %s", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for current pause to resume")
	}
	if session.ShouldTrigger(7) {
		t.Fatal("removed breakpoint should not trigger later safe points")
	}
}

func TestSessionConcurrentBreakpointMutation(t *testing.T) {
	session := NewSession()
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				line := (worker + i) % 17
				session.AddBreakpoint(line)
				_ = session.ShouldTrigger(line)
				session.RemoveBreakpoint(line)
				_ = session.ShouldTrigger(line)
			}
		}()
	}
	wg.Wait()
}
