package debugger

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	miniruntime "gopkg.d7z.net/go-mini/core/runtime"
)

func TestSessionBreakpointAndSteppingState(t *testing.T) {
	session := NewSession()
	sourcePoint := miniruntime.DebugPoint{RunID: 1, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 12}, FrameDepth: 1}
	otherModulePoint := miniruntime.DebugPoint{RunID: 1, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "other", F: "main.mgo", L: 12}, FrameDepth: 1}
	if session.Checkpoint(sourcePoint).Stop {
		t.Fatal("empty session should not trigger")
	}

	bp := Breakpoint{ModulePath: "main", File: "main.mgo", Line: 12}
	if err := session.AddBreakpoint(bp); err != nil {
		t.Fatal(err)
	}
	has, err := session.HasBreakpoint(bp)
	if err != nil {
		t.Fatal(err)
	}
	if !has || !session.Checkpoint(sourcePoint).Stop {
		t.Fatal("expected breakpoint to trigger")
	}
	if session.Checkpoint(otherModulePoint).Stop {
		t.Fatal("breakpoint should not trigger for another module")
	}

	if err := session.RemoveBreakpoint(bp); err != nil {
		t.Fatal(err)
	}
	has, err = session.HasBreakpoint(bp)
	if err != nil {
		t.Fatal(err)
	}
	if has || session.Checkpoint(sourcePoint).Stop {
		t.Fatal("removed breakpoint should not trigger")
	}

	session.Publish(&Event{RunID: 7, ExecutionContextID: 1, Loc: &Position{ModulePath: "main", F: "main.mgo", L: 98}, FrameDepth: 1})
	if err := session.RequestStep(miniruntime.DebugStepRequest{RunID: 7, Mode: miniruntime.DebugStepInto}); err != nil {
		t.Fatal(err)
	}
	if !session.HasStep(7) || !session.Checkpoint(miniruntime.DebugPoint{RunID: 7, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 99}, FrameDepth: 1}).Stop {
		t.Fatal("stepping should trigger for the selected run")
	}
	if session.HasStep(8) || session.Checkpoint(miniruntime.DebugPoint{RunID: 8, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 99}, FrameDepth: 1}).Stop {
		t.Fatal("stepping should not trigger for another run")
	}
	session.ClearStep(8)
	if !session.HasStep(7) {
		t.Fatal("clearing another run should not remove the active step")
	}
	session.ClearStep(7)
	if session.HasStep(7) || session.Checkpoint(miniruntime.DebugPoint{RunID: 7, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 99}, FrameDepth: 1}).Stop {
		t.Fatal("cleared stepping should not trigger")
	}
}

func TestSessionRejectsInvalidBreakpoint(t *testing.T) {
	session := NewSession()
	for _, bp := range []Breakpoint{
		{File: "main.mgo", Line: 1},
		{ModulePath: "main", Line: 1},
		{ModulePath: "main", File: "main.mgo"},
	} {
		if err := session.AddBreakpoint(bp); err == nil {
			t.Fatalf("expected invalid breakpoint to fail: %#v", bp)
		}
	}
}

func TestSessionStepOverUsesPausedFrameDepth(t *testing.T) {
	session := NewSession()
	session.Publish(&Event{
		RunID:              7,
		ExecutionContextID: 1,
		Loc:                &Position{ModulePath: "main", F: "main.mgo", L: 10},
		FrameDepth:         2,
	})
	if err := session.RequestStep(miniruntime.DebugStepRequest{RunID: 7, Mode: miniruntime.DebugStepOver}); err != nil {
		t.Fatal(err)
	}
	if session.Checkpoint(miniruntime.DebugPoint{RunID: 7, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "fmt", F: "fmt.mgo", L: 13}, FrameDepth: 3}).Stop {
		t.Fatal("step-over should not stop in deeper callee frames")
	}
	if session.Checkpoint(miniruntime.DebugPoint{RunID: 7, ExecutionContextID: 2, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 11}, FrameDepth: 2}).Stop {
		t.Fatal("step-over should not stop in another execution context")
	}
	if !session.Checkpoint(miniruntime.DebugPoint{RunID: 7, ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 11}, FrameDepth: 2}).Stop {
		t.Fatal("step-over should stop when returning to the original frame depth")
	}
}

func TestSessionBreakpointInterruptsActiveStep(t *testing.T) {
	session := NewSession()
	session.Publish(&Event{
		RunID:              7,
		ExecutionContextID: 1,
		Loc:                &Position{ModulePath: "main", F: "main.mgo", L: 10},
		FrameDepth:         1,
	})
	if err := session.RequestStep(miniruntime.DebugStepRequest{RunID: 7, Mode: miniruntime.DebugStepOver}); err != nil {
		t.Fatal(err)
	}
	if err := session.AddBreakpoint(Breakpoint{ModulePath: "main", File: "main.mgo", Line: 20}); err != nil {
		t.Fatal(err)
	}
	decision := session.Checkpoint(miniruntime.DebugPoint{
		RunID:              7,
		ExecutionContextID: 1,
		Loc:                miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: 20},
		FrameDepth:         2,
	})
	if !decision.Stop || decision.Reason != miniruntime.DebugStopBreakpoint || !decision.ClearStep {
		t.Fatalf("expected breakpoint to stop and clear active step, got %#v", decision)
	}
	session.ClearStep(7)
	if session.HasStep(7) {
		t.Fatal("active step should be cleared after breakpoint interruption")
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

func TestSessionPublishAfterCloseDoesNotRecordPause(t *testing.T) {
	session := NewSession()
	session.Close()
	session.Publish(&Event{
		RunID:              7,
		ExecutionContextID: 1,
		Loc:                &Position{ModulePath: "main", F: "main.mgo", L: 10},
		FrameDepth:         1,
	})
	if err := session.RequestStep(miniruntime.DebugStepRequest{RunID: 7, Mode: miniruntime.DebugStepInto}); err == nil {
		t.Fatal("closed session should not record pause points")
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
				if line == 0 {
					line = 17
				}
				bp := Breakpoint{ModulePath: "main", File: "main.mgo", Line: line}
				point := miniruntime.DebugPoint{RunID: uint64(worker + 1), ExecutionContextID: 1, Loc: miniruntime.DebugPosition{ModulePath: "main", F: "main.mgo", L: line}, FrameDepth: 1}
				if err := session.AddBreakpoint(bp); err != nil {
					t.Errorf("add breakpoint failed: %v", err)
					return
				}
				_ = session.Checkpoint(point)
				if err := session.RemoveBreakpoint(bp); err != nil {
					t.Errorf("remove breakpoint failed: %v", err)
					return
				}
				_ = session.Checkpoint(point)
			}
		}()
	}
	wg.Wait()
}
