package runtime

import "testing"

func TestFiberSchedulerRoundRobinKeepsFIFOOrder(t *testing.T) {
	scheduler := NewFiberScheduler()
	first := &VMFiber{ID: 1}
	second := &VMFiber{ID: 2}
	scheduler.runq.push(first)
	scheduler.runq.push(second)

	fiber, done, _ := scheduler.nextReady()
	if done || fiber != first {
		t.Fatalf("expected first fiber first, got fiber=%v done=%v", fiber, done)
	}
	if err := scheduler.YieldCurrent(); err != nil {
		t.Fatal(err)
	}

	fiber, done, _ = scheduler.nextReady()
	if done || fiber != second {
		t.Fatalf("expected second fiber next, got fiber=%v done=%v", fiber, done)
	}
	if err := scheduler.YieldCurrent(); err != nil {
		t.Fatal(err)
	}

	fiber, done, _ = scheduler.nextReady()
	if done || fiber != first {
		t.Fatalf("expected first fiber to return after round robin, got fiber=%v done=%v", fiber, done)
	}
}

func TestFiberSchedulerCompletionDrainBudgetPreservesRunnableFairness(t *testing.T) {
	const completions = completionDrainBudget + 44

	scheduler := NewFiberScheduler()
	ready := &VMFiber{ID: 1}
	scheduler.runq.push(ready)

	for i := 0; i < completions; i++ {
		token := uint64(i + 1)
		resume := Task{Op: OpResumeFFI, Data: &ResumeFFIData{}}
		scheduler.pending[token] = &suspendedFiber{
			Fiber:  &VMFiber{ID: uint32(i + 2)},
			Frame:  &FiberFrame{Session: &StackContext{}},
			Resume: resume,
		}
		if ok := scheduler.completeWire(token, []byte{byte(i)}, nil); !ok {
			t.Fatalf("failed to enqueue completion %d", token)
		}
	}

	fiber, done, _ := scheduler.nextReady()
	if done || fiber != ready {
		t.Fatalf("expected existing runnable fiber first, got fiber=%v done=%v", fiber, done)
	}
	if got := scheduler.runq.len(); got != completionDrainBudget {
		t.Fatalf("expected %d resumed fibers queued after draining one batch, got %d", completionDrainBudget, got)
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
	fiber, done, _ = scheduler.nextReady()
	if done {
		t.Fatal("scheduler should keep running after draining the first batch")
	}
	if fiber == nil || fiber.ID != 2 {
		t.Fatalf("expected FIFO completion resume order to start with fiber 2, got %#v", fiber)
	}
	if got := scheduler.completed.len(); got != 0 {
		t.Fatalf("expected remaining completions to be drained on the next scheduling round, got %d", got)
	}
}
