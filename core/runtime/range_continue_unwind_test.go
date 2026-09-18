package runtime

import (
	"context"
	"testing"
)

func TestRangeContinuePrunesResidualIterationTasks(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	sym := SymbolRef{Name: "state", Kind: SymbolLocal, Slot: 0}
	session.ScopeApply("fn")
	if err := session.DeclareSymbol(sym, MustParseRuntimeType("Int64")); err != nil {
		t.Fatalf("declare local failed: %v", err)
	}
	if err := session.StoreSymbol(sym, NewInt(0)); err != nil {
		t.Fatalf("store initial local failed: %v", err)
	}

	// This simulates the bad state observed in nested range continue unwinds:
	// a stale task from the current iteration survives between OpRangeIter and
	// the range marker boundary. Continue must discard it before resuming.
	session.TaskStack = append(session.TaskStack,
		Task{Op: OpLoopBoundary},
		Task{Op: OpStoreLocal, Data: sym},
		Task{Op: OpPush, Data: NewInt(9)},
	)
	session.UnwindMode = UnwindContinue

	handled, err := exec.handleUnwind(session, &Task{Op: OpRangeIter, Data: &RangeData{Length: 0, Index: 0}})
	if err != nil {
		t.Fatalf("handleUnwind failed: %v", err)
	}
	if !handled {
		t.Fatal("expected range continue unwind to be handled")
	}

	if err := exec.Run(session); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	got, err := session.LoadSymbol(sym)
	if err != nil {
		t.Fatalf("load local failed: %v", err)
	}
	if got == nil || got.I64 != 0 {
		t.Fatalf("stale iteration task was not pruned, got %#v", got)
	}
}
