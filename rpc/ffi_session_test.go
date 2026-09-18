package rpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/ffi"
)

func TestFFISessionRepeatedBindingCloseSharesOutcome(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("provider cleanup failed")
	lease := &blockingBindingLease{started: make(chan struct{}), release: make(chan struct{}), err: wantErr}
	unblock := sync.OnceFunc(func() { close(lease.release) })
	method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	binder, err := NewLocalBinder(LocalBinderOptions{}, localBinderProvider{
		contract: testContract(method),
		bind:     func(context.Context, BindRequest) (ProviderLease, error) { return lease, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := newFFISession(sessionConfig{Binder: binder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unblock(); _ = session.Shutdown(ctx) })
	id, err := session.open(ctx, testContract(method), BindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := session.beginBindingClose(id, false)
	<-lease.started
	second := session.beginBindingClose(id, false)
	unblock()
	for _, cleanup := range []*ffiLeaseCleanup{first, second} {
		if cleanup == nil {
			t.Fatal("retiring binding lost its pending cleanup")
		}
		<-cleanup.done
		if !errors.Is(cleanup.err, wantErr) {
			t.Fatalf("binding cleanup = %v", cleanup.err)
		}
	}
	if err := session.Shutdown(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("session cleanup = %v", err)
	}
}

func TestFFISessionOwnsLeaseAndCallsBoundRoute(t *testing.T) {
	method := Method{ID: "example.echo::Echo.Call", Service: "example.echo::Echo", Name: "Call", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(_ context.Context, values []Value) ([]Value, error) { return values, nil })
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newFFISession(sessionConfig{Binder: binder})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	invoke := func(request ffiRequest) ffiResponse { return invokeFFI(context.Background(), t, session, request) }
	opened := invoke(ffiRequest{Operation: "open", Contract: Contract{Protocol: ContractProtocol, Methods: []Method{method}}})
	if opened.Code != "" || opened.Lease == 0 {
		t.Fatalf("open = %#v", opened)
	}
	values, _ := EncodeValues([]Value{{Type: "string", Data: "hello"}}, Limits{})
	called := invoke(ffiRequest{Operation: "call", Lease: opened.Lease, Method: method, Payload: values})
	decoded, err := DecodeValues(called.Payload, Limits{})
	if err != nil || len(decoded) != 1 || decoded[0].Data != "hello" {
		t.Fatalf("call = %#v, %v", called, err)
	}
	closed := invoke(ffiRequest{Operation: "close", Lease: opened.Lease})
	if closed.Code != "" {
		t.Fatalf("close = %#v", closed)
	}
	rejected := invoke(ffiRequest{Operation: "call", Lease: opened.Lease, Method: method, Payload: values})
	if rejected.Code != CodeUnavailable {
		t.Fatalf("closed call = %#v", rejected)
	}
}

func TestFFISessionRunsUnaryInterceptorsInDeclarationOrder(t *testing.T) {
	method := Method{ID: "example.intercept::Service.Call", Service: "example.intercept::Service", Name: "Call", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(_ context.Context, values []Value) ([]Value, error) { return values, nil })
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	interceptor := func(name string) UnaryInterceptor {
		return func(ctx context.Context, call Call, next UnaryInvoker) (*Result, error) {
			order = append(order, name+":before")
			result, err := next(ctx, call)
			order = append(order, name+":after")
			return result, err
		}
	}
	session, err := newFFISession(sessionConfig{Binder: binder, Interceptors: []UnaryInterceptor{interceptor("first"), interceptor("second")}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open", Contract: testContract(method)})
	payload, err := EncodeValues([]Value{{Type: "string", Data: "value"}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	response := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "call", Lease: opened.Lease, Method: method, Payload: payload})
	if response.Code != "" {
		t.Fatalf("call response = %#v", response)
	}
	want := []string{"first:before", "second:before", "second:after", "first:after"}
	if len(order) != len(want) {
		t.Fatalf("interceptor order = %#v", order)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("interceptor order = %#v", order)
		}
	}
}

func TestFFISessionCloseLeaseReleasesAcceptedResources(t *testing.T) {
	open := Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: testContractHash}
	resource := &countedResource{}
	provider, err := NewProvider(MethodBinding{Method: open, Invoke: func(ctx context.Context, _ []Value) ([]Value, error) {
		value, exportErr := Export(ctx, resource, testResourceHash)
		return []Value{value}, exportErr
	}})
	if err != nil {
		t.Fatal(err)
	}
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newFFISession(sessionConfig{Binder: binder})
	if err != nil {
		t.Fatal(err)
	}
	contract := Contract{Protocol: ContractProtocol, Methods: []Method{open}}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open", Contract: contract})
	empty, err := EncodeValues(nil, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	called := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "call", Lease: opened.Lease, Method: open, Payload: empty})
	values, err := DecodeValues(called.Payload, Limits{})
	if err != nil || len(values) != 1 || values[0].Resource == nil {
		t.Fatalf("open resource = %#v, %v", called, err)
	}
	closed := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "close", Lease: opened.Lease})
	if closed.Code != "" || resource.closes.Load() != 1 {
		t.Fatalf("close = %#v, resource closes=%d", closed, resource.closes.Load())
	}
	if err := session.Close(); err != nil || resource.closes.Load() != 1 {
		t.Fatalf("session close = %v, resource closes=%d", err, resource.closes.Load())
	}
}

func TestFFISessionEnforcesSharedLeaseLimit(t *testing.T) {
	method := Method{ID: "example/capacity::Service.Call", Service: "example/capacity::Service", Name: "Call", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) { return nil, nil })
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	var publications atomic.Int32
	session, err := newFFISession(sessionConfig{
		Binder: binder,
		Limits: Limits{MaxBindings: 1},
		PublishProvider: func(context.Context, Provider) (func() error, error) {
			publications.Add(1)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	contract := Contract{Protocol: ContractProtocol, Methods: []Method{method}}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open", Contract: contract})
	rejected := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open_provider", Contract: contract})
	if rejected.Code != CodeResourceExhausted || publications.Load() != 0 {
		t.Fatalf("provider over capacity = %#v, publications=%d", rejected, publications.Load())
	}
	if closed := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "close", Lease: opened.Lease}); closed.Code != "" {
		t.Fatalf("close = %#v", closed)
	}
	accepted := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open_provider", Contract: contract})
	if accepted.Code != "" || publications.Load() != 1 {
		t.Fatalf("provider after release = %#v, publications=%d", accepted, publications.Load())
	}
}

func TestFFISessionRequestTimeoutCancelsProviderCall(t *testing.T) {
	method := Method{ID: "example.slow::Slow.Wait", Service: "example.slow::Slow", Name: "Wait", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newFFISession(sessionConfig{Binder: binder})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	contract := Contract{Protocol: ContractProtocol, Methods: []Method{method}}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open", Contract: contract})
	payload, err := EncodeValues(nil, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	response := invokeFFI(context.Background(), t, session, ffiRequest{
		Operation: "call", Lease: opened.Lease, Method: method, Payload: payload, TimeoutNanos: int64(time.Millisecond),
	})
	if response.Code != CodeDeadlineExceeded {
		t.Fatalf("timed call = %#v", response)
	}
}

func TestFFIShutdownWaitsForRetiringBindings(t *testing.T) {
	for _, provider := range []bool{false, true} {
		name := "lease"
		if provider {
			name = "provider"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
			lease := &blockingBindingLease{started: make(chan struct{}), release: make(chan struct{})}
			defer close(lease.release)
			local, err := NewLocalBinder(LocalBinderOptions{}, localBinderProvider{contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) { return lease, nil }})
			if err != nil {
				t.Fatal(err)
			}
			session, err := newFFISession(sessionConfig{Binder: local, Limits: Limits{MaxBindings: 1}, PublishProvider: func(context.Context, Provider) (func() error, error) { return lease.Close, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := session.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			})
			var id uint64
			if provider {
				id, err = session.openProvider(ctx, "service", testContract(method))
			} else {
				id, err = session.open(ctx, testContract(method), BindOptions{})
			}
			if err != nil {
				t.Fatal(err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := session.closeBinding(canceled, id, provider); !errors.Is(err, context.Canceled) {
				t.Fatalf("close = %v", err)
			}
			<-lease.started
			if err := session.reserveLease(); codeOf(err) != CodeResourceExhausted {
				t.Fatalf("retiring binding quota = %v", err)
			}
			if err := session.Shutdown(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown = %v", err)
			}
			select {
			case <-session.shutdownDone:
				t.Fatal("shutdown completed before resource cleanup")
			default:
			}
		})
	}
}

func TestFFIShutdownWaitsForCompletionDelivery(t *testing.T) {
	ctx := context.Background()
	method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) { return nil, nil })
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newFFISession(sessionConfig{Binder: binder})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	t.Cleanup(func() {
		if err := session.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	_, err = session.Start(ctx, ffi.Request{Route: FFIRoute, Payload: encodeFFIRequest(ffiRequest{Version: FFIProtocol, Operation: "open", Contract: testContract(method)})}, func(ffi.Result) { close(started); <-release })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := session.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown = %v", err)
	}
	select {
	case <-session.shutdownDone:
		t.Fatal("shutdown passed active completion")
	default:
	}
	if _, err := session.Start(ctx, ffi.Request{Route: FFIRoute}, func(ffi.Result) {}); err == nil || errors.Is(err, ffi.ErrRouteUnavailable) {
		t.Fatalf("start on closing session = %v", err)
	}
}
