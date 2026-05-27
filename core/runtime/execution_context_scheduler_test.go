package runtime

import "testing"

func TestSchedulerRoundRobinFIFO(t *testing.T) {
	scheduler := NewExecutionContextScheduler()
	first := &VMExecutionContext{ID: 1}
	second := &VMExecutionContext{ID: 2}
	scheduler.runq.push(first)
	scheduler.runq.push(second)

	snapshot := scheduler.Snapshot()
	if snapshot.State != SchedulerStateReady || snapshot.ExecCtx != first {
		t.Fatalf("expected first execution context first, got state=%d context=%v", snapshot.State, snapshot.ExecCtx)
	}
	if err := scheduler.YieldCurrent(); err != nil {
		t.Fatal(err)
	}

	snapshot = scheduler.Snapshot()
	if snapshot.State != SchedulerStateReady || snapshot.ExecCtx != second {
		t.Fatalf("expected second execution context next, got state=%d context=%v", snapshot.State, snapshot.ExecCtx)
	}
	if err := scheduler.YieldCurrent(); err != nil {
		t.Fatal(err)
	}

	snapshot = scheduler.Snapshot()
	if snapshot.State != SchedulerStateReady || snapshot.ExecCtx != first {
		t.Fatalf("expected first execution context to return after round robin, got state=%d context=%v", snapshot.State, snapshot.ExecCtx)
	}
}

func TestSchedulerDrainBudgetFairness(t *testing.T) {
	const completions = completionDrainBudget + 44

	scheduler := NewExecutionContextScheduler()
	ready := &VMExecutionContext{ID: 1}
	scheduler.runq.push(ready)

	for i := 0; i < completions; i++ {
		token := uint64(i + 1)
		resume := Task{Op: OpResumeFFI, Data: &ResumeFFIData{}}
		scheduler.pending[token] = &suspendedExecutionContext{
			ExecutionContext: &VMExecutionContext{ID: uint32(i + 2)},
			Frame:            &ExecutionContextFrame{Session: &StackContext{}},
			Resume:           resume,
		}
		if ok := scheduler.completeWire(token, []byte{byte(i)}, nil); !ok {
			t.Fatalf("failed to enqueue completion %d", token)
		}
	}

	snapshot := scheduler.Snapshot()
	if snapshot.State != SchedulerStateReady || snapshot.ExecCtx != ready {
		t.Fatalf("expected existing runnable execution context first, got state=%d context=%v", snapshot.State, snapshot.ExecCtx)
	}
	if got := scheduler.runq.len(); got != completionDrainBudget {
		t.Fatalf("expected %d resumed execution contexts queued after draining one batch, got %d", completionDrainBudget, got)
	}
	if got := scheduler.completed.len(); got != completions-completionDrainBudget {
		t.Fatalf("expected %d completions left for the next round, got %d", completions-completionDrainBudget, got)
	}
	if got := len(scheduler.pending); got != completions-completionDrainBudget {
		t.Fatalf("expected %d pending completions left, got %d", completions-completionDrainBudget, got)
	}
	if len(scheduler.wake) != 1 {
		t.Fatalf("expected scheduler wake signal for the remaining completion batch, got %d", len(scheduler.wake))
	}

	scheduler.FinishCurrent()
	snapshot = scheduler.Snapshot()
	if snapshot.State == SchedulerStateDone {
		t.Fatal("scheduler should keep running after draining the first batch")
	}
	if snapshot.ExecCtx == nil || snapshot.ExecCtx.ID != 2 {
		t.Fatalf("expected FIFO completion resume order to start with execution context 2, got %#v", snapshot.ExecCtx)
	}
	if got := scheduler.completed.len(); got != 0 {
		t.Fatalf("expected remaining completions to be drained on the next scheduling round, got %d", got)
	}
}
