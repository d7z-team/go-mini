package rpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ProviderResult carries values and an optional decision owned by a proxy
// provider. Direct providers leave the decision empty.
type ProviderResult struct {
	Values      []Value
	decide      func(bool) error
	once        sync.Once
	decisionErr error
}

// NewProviderResult creates a provisional result for proxy Provider implementations.
func NewProviderResult(values []Value, decide func(bool) error) *ProviderResult {
	return &ProviderResult{Values: cloneValues(values), decide: decide}
}

func (r *ProviderResult) accept() error  { return r.finish(true) }
func (r *ProviderResult) discard() error { return r.finish(false) }

func (r *ProviderResult) finish(accept bool) error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		defer func() {
			if failure := recover(); failure != nil {
				r.decisionErr = StatusError{Code: CodeInternal, Message: fmt.Sprintf("rpc provider result decision panic: %v", failure)}
			}
		}()
		if r.decide != nil {
			r.decisionErr = r.decide(accept)
		}
	})
	return r.decisionErr
}

// Result is a provisional RPC result. Exactly one of Accept or Discard is
// applied. A canceled waiter does not cancel an adopted decision.
type Result struct {
	Values []Value
	accept func(bool) error
	// release owns only resources created by this result, after Accept succeeds.
	// The FFI handoff uses it when the VM cannot consume the accepted reply.
	release     func() error
	mu          sync.Mutex
	done        chan struct{}
	accepted    bool
	waiters     int
	delivered   bool
	abandoned   bool
	cleaned     bool
	decisionErr error
}

func (r *Result) Accept(ctx context.Context) error  { return r.decide(ctx, true) }
func (r *Result) Discard(ctx context.Context) error { return r.decide(ctx, false) }

func (r *Result) decide(ctx context.Context, accept bool) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.done == nil {
		// An already canceled Accept must discard the provisional result.
		r.accepted = accept && ctx.Err() == nil
		r.done = make(chan struct{})
		go r.finishDecision()
	} else if r.accepted != accept {
		r.mu.Unlock()
		return StatusError{Code: CodeFailedPrecondition, Message: "rpc result already has a different decision"}
	}
	if r.abandoned && accept {
		r.mu.Unlock()
		return StatusError{Code: CodeCanceled, Message: "rpc result delivery was abandoned"}
	}
	r.waiters++
	done := r.done
	r.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
	}
	r.mu.Lock()
	r.waiters--
	if err := ctx.Err(); err != nil {
		if r.waiters == 0 && !r.delivered {
			r.abandoned = true
		}
		cleanup := r.claimAbandonedReleaseLocked()
		r.mu.Unlock()
		if cleanup != nil {
			go r.releaseAbandoned(cleanup)
		}
		return statusError(err)
	}
	err := r.decisionErr
	if err == nil {
		r.delivered = true
	}
	r.mu.Unlock()
	return err
}

func (r *Result) finishDecision() {
	var err error
	func() {
		defer func() {
			if failure := recover(); failure != nil {
				err = StatusError{Code: CodeInternal, Message: fmt.Sprintf("rpc result decision panic: %v", failure)}
			}
		}()
		if r.accept != nil {
			err = r.accept(r.accepted)
		}
	}()
	r.mu.Lock()
	r.decisionErr = err
	close(r.done)
	cleanup := r.claimAbandonedReleaseLocked()
	r.mu.Unlock()
	if cleanup != nil {
		r.releaseAbandoned(cleanup)
	}
}

func (r *Result) claimAbandonedReleaseLocked() func() error {
	select {
	case <-r.done:
	default:
		return nil
	}
	if !r.accepted || !r.abandoned || r.cleaned || r.decisionErr != nil {
		return nil
	}
	r.cleaned = true
	return r.release
}

func (r *Result) releaseAbandoned(release func() error) {
	err := release()
	r.mu.Lock()
	r.decisionErr = errors.Join(r.decisionErr, err)
	r.mu.Unlock()
}

// finishForShutdown joins an adopted decision instead of attempting its
// opposite. The RouteSet owns final resource reclamation after this returns.
func (r *Result) finishForShutdown() error {
	r.mu.Lock()
	if r.done == nil {
		r.done = make(chan struct{})
		go r.finishDecision()
	}
	done := r.done
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	err := r.decisionErr
	r.mu.Unlock()
	return err
}
