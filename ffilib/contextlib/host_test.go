package contextlib

import (
	"context"
	"testing"
	"time"
)

func TestTimerStateStopWithoutWaitersReturnsTrue(t *testing.T) {
	state := newTimerState(int64(time.Hour))
	if !state.stop() {
		t.Fatal("expected first stop to succeed")
	}
	if state.stop() {
		t.Fatal("expected second stop to report inactive timer")
	}
}

func TestTimerStateWaitCancelStopsTimer(t *testing.T) {
	state := newTimerState(int64(time.Hour))
	completed := make(chan bool, 1)
	wait, err := state.wait().Start(context.Background(), waitCancelCompletion{done: completed})
	if err != nil {
		t.Fatal(err)
	}
	if wait == nil {
		t.Fatal("expected wait handle")
	}
	wait.Cancel()

	state.mu.Lock()
	stopped := state.stopped
	timerNil := state.timer == nil
	waiters := len(state.waiters)
	state.mu.Unlock()
	if !stopped || !timerNil || waiters != 0 {
		t.Fatalf("expected canceled timer to be stopped and detached, stopped=%t timerNil=%t waiters=%d", stopped, timerNil, waiters)
	}
	if state.stop() {
		t.Fatal("expected canceled timer to report inactive timer")
	}
	select {
	case value := <-completed:
		t.Fatalf("canceled waiter should not complete, got %t", value)
	default:
	}
}

type waitCancelCompletion struct {
	done chan<- bool
}

func (c waitCancelCompletion) Complete(value bool, _ error) bool {
	select {
	case c.done <- value:
	default:
	}
	return true
}
