package rpc

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestHostShutdownJoinsConcurrentSessionFailures(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	leases := []*blockingBindingLease{
		{started: make(chan struct{}), release: release, err: errors.New("first cleanup failed")},
		{started: make(chan struct{}), release: release, err: errors.New("second cleanup failed")},
	}
	method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	next := 0
	host, err := NewHost(HostOptions{Providers: []Provider{localBinderProvider{
		contract: testContract(method),
		bind: func(context.Context, BindRequest) (ProviderLease, error) {
			lease := leases[next]
			next++
			return lease, nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unblock(); _ = host.Close() })
	for range leases {
		session, err := host.Open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.(*ffiSession).open(ctx, testContract(method), BindOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := host.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Both cleanups must start before either is allowed to complete.
	for _, lease := range leases {
		<-lease.started
	}
	unblock()
	for range 2 {
		err := host.Close()
		for _, lease := range leases {
			if !errors.Is(err, lease.err) {
				t.Fatalf("shutdown = %v, missing %v", err, lease.err)
			}
		}
	}
}

type binderFunc func(context.Context, BindRequest) (*RouteSet, error)

func (f binderFunc) Bind(ctx context.Context, request BindRequest) (*RouteSet, error) {
	return f(ctx, request)
}

func TestHostFallbackOnlyHandlesUnimplementedBindings(t *testing.T) {
	want := errors.New("fallback result")
	fallbackCalls := 0
	fallback := binderFunc(func(context.Context, BindRequest) (*RouteSet, error) {
		fallbackCalls++
		return nil, want
	})

	binder := fallbackBinder{
		primary: binderFunc(func(context.Context, BindRequest) (*RouteSet, error) {
			return nil, StatusError{Code: CodeUnimplemented, Message: "not local"}
		}),
		fallback: fallback,
	}
	if _, err := binder.Bind(context.Background(), BindRequest{}); !errors.Is(err, want) {
		t.Fatalf("fallback error = %v, want %v", err, want)
	}
	if fallbackCalls != 1 {
		t.Fatalf("fallback call count = %d, want 1", fallbackCalls)
	}

	for _, code := range []Code{CodeInvalidArgument, CodeFailedPrecondition, CodePermissionDenied, CodeUnavailable, CodeDeadlineExceeded, CodeCanceled} {
		binder.primary = binderFunc(func(context.Context, BindRequest) (*RouteSet, error) {
			return nil, StatusError{Code: code, Message: "binding failed"}
		})
		if _, err := binder.Bind(context.Background(), BindRequest{}); codeOf(err) != code {
			t.Fatalf("primary error = %v, want %s", err, code)
		}
		if fallbackCalls != 1 {
			t.Fatalf("fallback handled %s: calls = %d", code, fallbackCalls)
		}
	}
}

func TestHostOwnsSessionLifecycle(t *testing.T) {
	host, err := NewHost(HostOptions{Fallback: binderFunc(func(context.Context, BindRequest) (*RouteSet, error) {
		return nil, StatusError{Code: CodeUnavailable, Message: "offline"}
	})})
	if err != nil {
		t.Fatal(err)
	}
	first, err := host.Open(context.Background())
	if err != nil || first == nil {
		t.Fatalf("open session: %v", err)
	}
	second, err := host.Open(context.Background())
	if err != nil || second == nil {
		t.Fatalf("open second session: %v", err)
	}
	sessionCount := func() int {
		host.mu.Lock()
		defer host.mu.Unlock()
		return len(host.sessions)
	}
	if count := sessionCount(); count != 2 {
		t.Fatalf("host sessions = %d", count)
	}
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count := sessionCount(); count != 1 {
		t.Fatalf("host sessions after first shutdown = %d", count)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if count := sessionCount(); count != 0 {
		t.Fatalf("host sessions after close = %d", count)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
}
