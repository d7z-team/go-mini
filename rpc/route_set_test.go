package rpc

import (
	"context"
	"errors"
	"testing"
	"time"
)

type abortOrderingResource struct {
	started    chan struct{}
	invokeDone chan struct{}
	closed     chan struct{}
	violation  chan struct{}
}

func (r *abortOrderingResource) Invoke(ctx context.Context, _ string, _ []Value) ([]Value, error) {
	close(r.started)
	<-ctx.Done()
	close(r.invokeDone)
	return nil, ctx.Err()
}

func (r *abortOrderingResource) Close(context.Context) error {
	select {
	case <-r.invokeDone:
	default:
		r.violation <- struct{}{}
	}
	close(r.closed)
	return nil
}

func TestRouteSetOwnsResourceTransaction(t *testing.T) {
	open := Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: testContractHash}
	read := Method{ID: "example/files::File.Read", Service: "example/files::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
	provider, err := NewProvider(MethodBinding{Method: open, Invoke: func(ctx context.Context, _ []Value) ([]Value, error) {
		value, err := Export(ctx, &testResource{}, testResourceHash)
		return []Value{value}, err
	}}, MethodBinding{Method: read})
	if err != nil {
		t.Fatal(err)
	}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(provider); err != nil {
		t.Fatal(err)
	}
	routes, err := gateway.Bind(context.Background(), testBindRequest(open, read))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := routes.Call(context.Background(), Call{Method: open})
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	ref := opened.Values[0].Resource
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := routes.Call(context.Background(), Call{Method: read, Receiver: ref})
	if err != nil {
		t.Fatal(err)
	}
	if result.Values[0].Data != "value" {
		t.Fatalf("read = %#v", result.Values)
	}
	_ = result.Discard(context.Background())
	if err := routes.Drop(context.Background(), *ref); err != nil {
		t.Fatal(err)
	}
}

func TestRouteSetsRejectUnboundResourceMethodWithoutMutation(t *testing.T) {
	open := Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: testContractHash}
	read := Method{ID: "example/files::File.Read", Service: "example/files::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
	ref := ResourceRef{Epoch: 1, ObjectID: 1, TypeHash: testResourceHash}
	remoteCalled := false
	sets := []*RouteSet{
		newRouteSet(1, normalizeLimits(Limits{}), []Method{open}, nil),
		newRemoteRouteSet(1, normalizeLimits(Limits{}), []Method{open}, &remoteBinding{call: func(context.Context, Call) (*Result, error) {
			remoteCalled = true
			return nil, nil
		}}),
	}
	sets[0].resources[ref.ObjectID] = &providerResource{resource: &testResource{}, typeHash: ref.TypeHash, active: true}
	sets[1].remoteResources[ref.ObjectID] = ref
	for _, routes := range sets {
		beforeResources := len(routes.resources) + len(routes.remoteResources)
		beforeCalls, beforeResults, beforeNextCall := routes.pendingCalls, routes.pendingResults, routes.nextCall
		if _, err := routes.Call(context.Background(), Call{Method: read, Receiver: &ref}); err == nil {
			t.Fatal("RouteSet accepted an unbound resource method")
		}
		if len(routes.resources)+len(routes.remoteResources) != beforeResources || routes.pendingCalls != beforeCalls || routes.pendingResults != beforeResults || routes.nextCall != beforeNextCall {
			t.Fatal("rejected resource call changed RouteSet state")
		}
	}
	if remoteCalled {
		t.Fatal("remote RouteSet sent an unbound resource method")
	}
}

func TestRouteSetAbortWaitsForResourceInvocationBeforeClose(t *testing.T) {
	open := Method{ID: "example/order::Files.Open", Service: "example/order::Files", Name: "Open", ContractHash: testContractHash}
	read := Method{ID: "example/order::File.Read", Service: "example/order::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
	resource := &abortOrderingResource{
		started: make(chan struct{}), invokeDone: make(chan struct{}),
		closed: make(chan struct{}), violation: make(chan struct{}, 1),
	}
	provider, err := NewProvider(
		MethodBinding{Method: open, Invoke: func(ctx context.Context, _ []Value) ([]Value, error) {
			value, err := Export(ctx, resource, testResourceHash)
			return []Value{value}, err
		}},
		MethodBinding{Method: read},
	)
	if err != nil {
		t.Fatal(err)
	}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(provider); err != nil {
		t.Fatal(err)
	}
	routes, err := gateway.Bind(context.Background(), testBindRequest(open, read))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := routes.Call(context.Background(), Call{Method: open})
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	ref := opened.Values[0].Resource
	callDone := make(chan error, 1)
	go func() {
		_, callErr := routes.Call(context.Background(), Call{Method: read, Receiver: ref})
		callDone <- callErr
	}()
	<-resource.started
	abortDone := make(chan error, 1)
	go func() { abortDone <- routes.Abort() }()
	if err := <-callDone; codeOf(err) != CodeCanceled {
		t.Fatalf("resource call error = %v", err)
	}
	if err := <-abortDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-resource.closed:
	default:
		t.Fatal("resource was not closed")
	}
	select {
	case <-resource.violation:
		t.Fatal("resource was closed before Invoke returned")
	default:
	}
}

func TestRouteSetShutdownTimeoutDoesNotAbandonCleanup(t *testing.T) {
	method := Method{ID: "example/shutdown::Service.Wait", Service: "example/shutdown::Service", Name: "Wait", ContractHash: testContractHash}
	started := make(chan struct{})
	release := make(chan struct{})
	provider := newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		close(started)
		<-release
		return nil, nil
	})
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() {
		_, callErr := routes.Call(context.Background(), Call{Method: method})
		callDone <- callErr
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := routes.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown timeout = %v", err)
	}
	close(release)
	if err := routes.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes.mu.Lock()
	if routes.methods != nil || routes.resources != nil || routes.resourceOrder != nil {
		routes.mu.Unlock()
		t.Fatal("closed route set retained method or resource state")
	}
	routes.mu.Unlock()
	if err := <-callDone; err != nil {
		t.Fatalf("completed call = %v", err)
	}
}

func TestRouteSetPendingResultConsumesCallCapacity(t *testing.T) {
	method := Method{ID: "example/capacity::Service.Value", Service: "example/capacity::Service", Name: "Value", ContractHash: testContractHash}
	gateway := newTestBinder(testBinderOptions{Limits: Limits{MaxPendingCalls: 1}})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		return []Value{{Type: "string", Data: "value"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	routes, err := gateway.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	first, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routes.Call(context.Background(), Call{Method: method}); codeOf(err) != CodeResourceExhausted {
		t.Fatalf("call with undecided result = %v", err)
	}
	if err := first.Discard(context.Background()); err != nil {
		t.Fatal(err)
	}
	third, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatalf("call after decision: %v", err)
	}
	if err := third.Discard(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRouteSetRejectsExhaustedObjectIDs(t *testing.T) {
	method := Method{ID: "example/ids::Service.Value", Service: "example/ids::Service", Name: "Value", ContractHash: testContractHash}
	routes := newRouteSet(1, normalizeLimits(Limits{}), []Method{method}, nil)
	routes.nextCall = ^uint64(0)
	if _, err := routes.Call(context.Background(), Call{Method: method}); codeOf(err) != CodeResourceExhausted {
		t.Fatalf("exhausted call id error = %v", err)
	}
	routes.nextResource = ^uint64(0)
	exporter := routeExporter{routes: routes, callID: 1}
	if _, err := exporter.Export(&testResource{}, testResourceHash); codeOf(err) != CodeResourceExhausted {
		t.Fatalf("exhausted resource id error = %v", err)
	}
}
