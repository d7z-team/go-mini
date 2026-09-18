package runtime

import (
	"context"
	"errors"
)

// InstanceState is the externally observable lifecycle of an Instance.
type InstanceState string

const (
	InstanceOpen    InstanceState = "open"
	InstanceFaulted InstanceState = "faulted"
	InstanceClosing InstanceState = "closing"
	InstanceClosed  InstanceState = "closed"
)

// ScopeStats is an immutable snapshot of one invocation and its derived work.
type ScopeStats struct {
	ID        int64
	Started   RevisionInfo
	RootState ExecutionState
	Tasks     int
	Timers    int
	FFICalls  int
	Steps     int64
	Done      bool
	Err       error
}

// Stats is a point-in-time runtime snapshot copied by the VM owner.
type Stats struct {
	Revision             RevisionInfo
	State                InstanceState
	ActiveScopes         int
	RunnableTasks        int
	BlockedTasks         int
	BlockedContexts      []BlockedContext
	PausedTasks          int
	PendingFFICalls      int
	Timers               int
	RetainedRevisions    int
	ExecutedSteps        int64
	LiveGuestBytes       int64
	AllocatedSinceSweep  int64
	TotalAllocatedBytes  int64
	PeakGuestBytes       int64
	PendingBoundaryBytes int64
	DynamicTypes         int
	DynamicTypeBytes     int64
}

// State returns the current instance lifecycle without entering the VM owner.
func (i *Instance) State() InstanceState {
	switch i.lifecycleState() {
	case instanceOpen:
		return InstanceOpen
	case instanceFaulted:
		return InstanceFaulted
	case instanceClosing:
		return InstanceClosing
	default:
		return InstanceClosed
	}
}

// RuntimeStats returns a stable snapshot without exposing scheduler state.
func (i *Instance) RuntimeStats(ctx context.Context) (Stats, error) {
	if i == nil || i.vm == nil {
		return Stats{}, errors.New("instance is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := i.vm.enterOwnerContext(ctx); err != nil {
		return Stats{}, err
	}
	defer i.vm.leaveOwner()
	sinceSweep := i.vm.allocatedSinceSweep.Load()
	stats := Stats{
		State: i.State(), PendingFFICalls: len(i.vm.ffiCalls), Timers: len(i.vm.timers),
		RetainedRevisions: i.vm.liveRevisionCount(), ExecutedSteps: i.vm.executedSteps,
		LiveGuestBytes: i.vm.liveGuestBytes.Load() + sinceSweep, AllocatedSinceSweep: sinceSweep,
		TotalAllocatedBytes: i.vm.totalAllocatedBytes.Load(), PeakGuestBytes: i.vm.peakGuestBytes.Load(),
		PendingBoundaryBytes: i.vm.pendingBoundaryBytes.Load(),
		DynamicTypes:         i.vm.dynamicTypeCount, DynamicTypeBytes: i.vm.dynamicTypeBytes,
	}
	if revision := i.vm.revision.Load(); revision != nil && revision.code != nil {
		stats.Revision = RevisionInfo{Generation: revision.generation, Hash: revision.code.image.Hash, SymbolsHash: revision.symbolsHash()}
	}
	if machine := i.vm.machine; machine != nil {
		stats.ActiveScopes = len(machine.scopes)
		stats.RunnableTasks = machine.runnableCount()
		stats.BlockedTasks, stats.BlockedContexts = machine.blockedContextSnapshot()
		if machine.paused != nil {
			stats.PausedTasks = 1
		}
	}
	return stats, nil
}
