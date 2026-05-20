package runtime

import "testing"

func TestExecutionContextSchedulerRoundRobinKeepsFIFOOrder(t *testing.T) {
	scheduler := NewExecutionContextScheduler()
	first := &VMExecutionContext{ID: 1}
	second := &VMExecutionContext{ID: 2}
	scheduler.runq.push(first)
	scheduler.runq.push(second)

	execCtx, done, _ := scheduler.nextReady()
	if done || execCtx != first {
		t.Fatalf("expected first execution context first, got context=%v done=%v", execCtx, done)
	}
	if err := scheduler.YieldCurrent(); err != nil {
		t.Fatal(err)
	}

	execCtx, done, _ = scheduler.nextReady()
	if done || execCtx != second {
		t.Fatalf("expected second execution context next, got context=%v done=%v", execCtx, done)
	}
	if err := scheduler.YieldCurrent(); err != nil {
		t.Fatal(err)
	}

	execCtx, done, _ = scheduler.nextReady()
	if done || execCtx != first {
		t.Fatalf("expected first execution context to return after round robin, got context=%v done=%v", execCtx, done)
	}
}

func TestExecutionContextSchedulerCompletionDrainBudgetPreservesRunnableFairness(t *testing.T) {
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

	execCtx, done, _ := scheduler.nextReady()
	if done || execCtx != ready {
		t.Fatalf("expected existing runnable execution context first, got context=%v done=%v", execCtx, done)
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
	execCtx, done, _ = scheduler.nextReady()
	if done {
		t.Fatal("scheduler should keep running after draining the first batch")
	}
	if execCtx == nil || execCtx.ID != 2 {
		t.Fatalf("expected FIFO completion resume order to start with execution context 2, got %#v", execCtx)
	}
	if got := scheduler.completed.len(); got != 0 {
		t.Fatalf("expected remaining completions to be drained on the next scheduling round, got %d", got)
	}
}
