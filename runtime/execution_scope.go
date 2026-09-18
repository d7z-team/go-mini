package runtime

import (
	"context"
	"errors"
)

// ScopeDone closes after all tasks, timers, and FFI calls created by this
// invocation have reached a terminal state. It can close after Wait returns.
func (e *Execution) ScopeDone() <-chan struct{} {
	if e == nil {
		return executionReady
	}
	e.mu.RLock()
	done := e.scopeDone
	e.mu.RUnlock()
	if done == nil {
		return executionReady
	}
	return done
}

// WaitScope waits for the invocation root and all work derived from it.
func (e *Execution) WaitScope(ctx context.Context) (ScopeStats, error) {
	if e == nil || e.instance == nil {
		return ScopeStats{}, errors.New("invalid execution")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-e.ScopeDone():
		stats, err := e.ScopeStats(ctx)
		if err != nil {
			return ScopeStats{}, err
		}
		return stats, stats.Err
	case <-ctx.Done():
		return ScopeStats{}, ctx.Err()
	}
}

// ScopeStats returns an immutable snapshot of work owned by this invocation.
func (e *Execution) ScopeStats(ctx context.Context) (ScopeStats, error) {
	if e == nil || e.instance == nil || e.instance.vm == nil {
		return ScopeStats{}, errors.New("invalid execution")
	}
	e.mu.RLock()
	if e.scopeFinalSet {
		stats := e.scopeFinal
		e.mu.RUnlock()
		return stats, nil
	}
	e.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := e.instance.vm.enterOwnerContext(ctx); err != nil {
		return ScopeStats{}, err
	}
	defer e.instance.vm.leaveOwner()
	if machine := e.instance.vm.machine; machine != nil {
		if scope := machine.scope(e.scopeID); scope != nil {
			return scope.snapshot(), nil
		}
	}
	e.mu.RLock()
	stats, ok := e.scopeFinal, e.scopeFinalSet
	e.mu.RUnlock()
	if ok {
		return stats, nil
	}
	return ScopeStats{}, errors.New("execution scope is unavailable")
}

func (e *Execution) setScopeFinal(stats ScopeStats) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.scopeFinal = stats
	e.scopeFinalSet = true
	e.mu.Unlock()
}
