package debugger

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSessionBreakpointAndSteppingState(t *testing.T) {
	session := NewSession()
	if session.ShouldTrigger(1, 12) {
		t.Fatal("empty session should not trigger")
	}

	session.AddBreakpoint(12)
	if !session.HasBreakpoint(12) || !session.ShouldTrigger(1, 12) {
		t.Fatal("expected breakpoint to trigger")
	}

	session.RemoveBreakpoint(12)
	if session.HasBreakpoint(12) || session.ShouldTrigger(1, 12) {
		t.Fatal("removed breakpoint should not trigger")
	}

	session.RequestStep(7)
	if !session.HasStep(7) || !session.ShouldTrigger(7, 99) {
		t.Fatal("stepping should trigger for the selected run")
	}
	if session.HasStep(8) || session.ShouldTrigger(8, 99) {
		t.Fatal("stepping should not trigger for another run")
	}
	session.ClearStep(8)
	if !session.HasStep(7) {
		t.Fatal("clearing another run should not remove the active step")
	}
	session.ClearStep(7)
	if session.HasStep(7) || session.ShouldTrigger(7, 99) {
		t.Fatal("cleared stepping should not trigger")
	}
}

func TestSessionPublishDeliversEvents(t *testing.T) {
	session := NewSession()
	session.Publish(&Event{RunID: 7, ExecutionContextID: 1, Loc: &Position{L: 7}})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := session.NextEvent(ctx)
	if err != nil {
		t.Fatalf("next event failed: %v", err)
	}
	if event.RunID != 7 || event.Loc == nil || event.Loc.L != 7 {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestSessionPublishIsNonBlockingAndOrdered(t *testing.T) {
	session := NewSession()
	for i := 0; i < 128; i++ {
		session.Publish(&Event{RunID: uint64(i + 1)})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 128; i++ {
		event, err := session.NextEvent(ctx)
		if err != nil {
			t.Fatalf("next event %d failed: %v", i, err)
		}
		if event.RunID != uint64(i+1) {
			t.Fatalf("event %d run id = %d, want %d", i, event.RunID, i+1)
		}
	}
}

func TestSessionCloseUnblocksNextEvent(t *testing.T) {
	session := NewSession()
	done := make(chan error, 1)
	go func() {
		_, err := session.NextEvent(context.Background())
		done <- err
	}()

	session.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("NextEvent after close returned %v, want ErrSessionClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for NextEvent to unblock")
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
				_ = session.ShouldTrigger(uint64(worker+1), line)
				session.RemoveBreakpoint(line)
				_ = session.ShouldTrigger(uint64(worker+1), line)
			}
		}()
	}
	wg.Wait()
}
