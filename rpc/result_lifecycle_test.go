package rpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResultCanceledAcceptReleasesUndeliveredResources(t *testing.T) {
	started, finish, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var decisions atomic.Int32
	result := &Result{accept: func(accept bool) error {
		if !accept {
			t.Error("adopted accept became discard")
		}
		decisions.Add(1)
		close(started)
		<-finish
		return nil
	}, release: func() error { close(released); return nil }}
	ctx, cancel := context.WithCancel(t.Context())
	outcome := make(chan error, 1)
	go func() { outcome <- result.Accept(ctx) }()
	<-started
	cancel()
	if code, _ := CodeOf(<-outcome); code != CodeCanceled {
		t.Fatalf("cancel code = %s", code)
	}
	close(finish)
	<-released
	if err := result.Accept(t.Context()); err == nil {
		t.Fatal("abandoned result was delivered again")
	}
	if decisions.Load() != 1 {
		t.Fatal("decision repeated")
	}
}

func TestResultCanceledWaiterDoesNotRevokeAnotherWaiter(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var released atomic.Int32
	result := &Result{accept: func(bool) error { close(started); <-finish; return nil }, release: func() error { released.Add(1); return nil }}
	ctx, cancel := context.WithCancel(t.Context())
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- result.Accept(ctx) }()
	<-started
	secondContext := &resultWaitContext{Context: t.Context(), waiting: make(chan struct{})}
	go func() { second <- result.Accept(secondContext) }()
	<-secondContext.waiting
	cancel()
	if code, _ := CodeOf(<-first); code != CodeCanceled {
		t.Fatalf("cancel code = %s", code)
	}
	close(finish)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if released.Load() != 0 {
		t.Fatal("live waiter lost accepted resources")
	}
}

// Done is evaluated after Accept registers the waiter and enters its select.
type resultWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *resultWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestResultCanceledBeforeAcceptDiscards(t *testing.T) {
	decided := make(chan bool, 1)
	result := &Result{accept: func(accept bool) error { decided <- accept; return nil }}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code, _ := CodeOf(result.Accept(ctx)); code != CodeCanceled {
		t.Fatalf("cancel code = %s", code)
	}
	if <-decided {
		t.Fatal("canceled accept committed resources")
	}
	if err := result.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type failedDecisionLease struct {
	resource        *retryCloseResource
	panicOnDecision bool
}

func (l *failedDecisionLease) Invoke(ctx context.Context, _ Method, _ []Value) (*ProviderResult, error) {
	value, err := Export(ctx, l.resource, testResourceHash)
	if err != nil {
		return nil, err
	}
	return NewProviderResult([]Value{value}, func(bool) error {
		if l.panicOnDecision {
			panic("decision failed")
		}
		return StatusError{Code: CodeUnavailable, Message: "decision failed"}
	}), nil
}

func (*failedDecisionLease) Close() error { return nil }

func TestResultFailedProviderDecisionReclaimsProvisionalResources(t *testing.T) {
	for _, accept := range []bool{false, true} {
		for _, panicOnDecision := range []bool{false, true} {
			resource := &retryCloseResource{}
			lease := &failedDecisionLease{resource: resource, panicOnDecision: panicOnDecision}
			method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
			binder, err := NewLocalBinder(LocalBinderOptions{}, localBinderProvider{
				contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) { return lease, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			routes, err := binder.Bind(t.Context(), testBindRequest(method))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = routes.Shutdown(context.Background()) })
			result, err := routes.Call(t.Context(), Call{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			if accept {
				err = result.Accept(t.Context())
			} else {
				err = result.Discard(t.Context())
			}
			want := CodeUnavailable
			if panicOnDecision {
				want = CodeInternal
			}
			if code, _ := CodeOf(err); code != want {
				t.Fatalf("accept=%v panic=%v: %v", accept, panicOnDecision, err)
			}
			if resource.attempts.Load() != 1 {
				t.Fatal("failed decision retained provisional resource")
			}
		}
	}
}

type resultDecisionLease struct {
	started chan bool
	finish  chan struct{}
	closed  chan struct{}
}

func (l *resultDecisionLease) Invoke(context.Context, Method, []Value) (*ProviderResult, error) {
	return NewProviderResult(nil, func(accept bool) error {
		l.started <- accept
		<-l.finish
		return nil
	}), nil
}

func (l *resultDecisionLease) Close() error { close(l.closed); return nil }

func TestRouteSetShutdownWaitsForOwnedResultDecision(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "provisional", true: "accepted"}[accept], func(t *testing.T) {
			lease := &resultDecisionLease{started: make(chan bool, 1), finish: make(chan struct{}), closed: make(chan struct{})}
			finish := sync.OnceFunc(func() { close(lease.finish) })
			method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
			binder, err := NewLocalBinder(LocalBinderOptions{}, localBinderProvider{
				contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) { return lease, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			routes, err := binder.Bind(t.Context(), testBindRequest(method))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { finish(); _ = routes.Shutdown(context.Background()) })
			result, err := routes.Call(t.Context(), Call{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			accepted := make(chan error, 1)
			if accept {
				go func() { accepted <- result.Accept(t.Context()) }()
				if !<-lease.started {
					t.Fatal("accept was discarded")
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := routes.Shutdown(ctx); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if !accept && <-lease.started {
				t.Fatal("shutdown accepted provisional result")
			}
			select {
			case <-lease.closed:
				t.Fatal("provider closed before owned decision finished")
			default:
			}
			finish()
			if err := routes.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			if accept {
				if err := <-accepted; err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-lease.closed:
			default:
				t.Fatal("provider still open after Shutdown")
			}
		})
	}
}
