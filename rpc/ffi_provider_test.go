package rpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/ffi"
)

func TestFFIProviderPublishesPullRequests(t *testing.T) {
	method := Method{ID: "example.echo::Echo.Call", Service: "example.echo::Echo", Name: "Call", ContractHash: testContractHash}
	published := make(chan Provider, 1)
	var releases atomic.Int32
	session, err := newFFISession(sessionConfig{PublishProvider: func(_ context.Context, provider Provider) (func() error, error) {
		published <- provider
		return func() error { releases.Add(1); return nil }, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	contract := Contract{Protocol: ContractProtocol, Methods: []Method{method}}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open_provider", Target: "guest", Contract: contract})
	if opened.Code != "" || opened.Lease == 0 {
		t.Fatalf("open provider = %#v", opened)
	}
	provider := <-published
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(context.Background(), BindRequest{Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	callResult := make(chan *Result, 1)
	callError := make(chan error, 1)
	go func() {
		result, callErr := routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: "string", Data: "request"}}})
		callResult <- result
		callError <- callErr
	}()
	accepted := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "accept", Lease: opened.Lease})
	arguments, err := DecodeValues(accepted.Payload, Limits{})
	if err != nil || accepted.Operation != "call" || accepted.Method != method || len(arguments) != 1 || arguments[0].Data != "request" {
		t.Fatalf("accept = %#v, arguments=%#v, err=%v", accepted, arguments, err)
	}
	values, err := EncodeValues([]Value{{Type: "string", Data: "response"}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	responded := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "respond", Lease: opened.Lease, RequestID: accepted.RequestID, Payload: values})
	if responded.Code != "" {
		t.Fatalf("respond = %#v", responded)
	}
	result, callErr := <-callResult, <-callError
	if callErr != nil || len(result.Values) != 1 || result.Values[0].Data != "response" {
		t.Fatalf("call = %#v, %v", result, callErr)
	}
	if err := result.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	closed := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "close_provider", Lease: opened.Lease})
	if closed.Code != "" || releases.Load() != 1 {
		t.Fatalf("close = %#v, releases=%d", closed, releases.Load())
	}
	if err := session.Close(); err != nil || releases.Load() != 1 {
		t.Fatalf("session close = %v, releases=%d", err, releases.Load())
	}
}

func TestFFIProviderBoundsHandoffQueues(t *testing.T) {
	method := Method{ID: "example.memory::Service.Call", Service: "example.memory::Service", Name: "Call", ContractHash: testContractHash}
	published := make(chan *ffiProvider, 1)
	session, err := newFFISession(sessionConfig{PublishProvider: func(_ context.Context, provider Provider) (func() error, error) {
		published <- provider.(*ffiProvider)
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{
		Operation: "open_provider",
		Contract:  testContract(method),
	})
	if opened.Code != "" {
		t.Fatalf("open provider = %#v", opened)
	}
	provider := <-published
	if cap(provider.events) >= provider.limits.MaxPendingCalls || cap(provider.cancel) != 1 {
		t.Fatalf("provider queues are not bounded handoffs: events=%d cancel=%d", cap(provider.events), cap(provider.cancel))
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.leases != nil || session.providers != nil {
		t.Fatal("closed FFI session retained leases")
	}
}

func TestFFIProviderCancellationRejectsLateResponse(t *testing.T) {
	method := Method{ID: "example.echo::Echo.Call", Service: "example.echo::Echo", Name: "Call", ContractHash: testContractHash}
	published := make(chan Provider, 1)
	session, err := newFFISession(sessionConfig{PublishProvider: func(_ context.Context, provider Provider) (func() error, error) {
		published <- provider
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	contract := Contract{Protocol: ContractProtocol, Methods: []Method{method}}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open_provider", Contract: contract})
	binder, err := NewLocalBinder(LocalBinderOptions{}, <-published)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(context.Background(), BindRequest{Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithCancel(context.Background())
	callDone := make(chan error, 1)
	go func() {
		_, callErr := routes.Call(callCtx, Call{Method: method})
		callDone <- callErr
	}()
	accepted := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "accept", Lease: opened.Lease})
	cancel()
	select {
	case callErr := <-callDone:
		code, _ := CodeOf(callErr)
		if !errors.Is(callErr, context.Canceled) && code != CodeCanceled {
			t.Fatalf("call cancellation = %v", callErr)
		}
	case <-time.After(time.Second):
		t.Fatal("guest provider call did not cancel")
	}
	canceled := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "accept", Lease: opened.Lease})
	if canceled.Operation != "cancel" || canceled.RequestID != accepted.RequestID {
		t.Fatalf("cancel event = %#v", canceled)
	}
	late := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "respond", Lease: opened.Lease, RequestID: accepted.RequestID})
	if late.Code != CodeNotFound {
		t.Fatalf("late response = %#v", late)
	}
}

func TestFFIProviderCancellationBeforeAcceptIsDelivered(t *testing.T) {
	method := Method{ID: "example.cancel::Service.Call", Service: "example.cancel::Service", Name: "Call", ContractHash: testContractHash}
	published := make(chan Provider, 1)
	session, err := newFFISession(sessionConfig{
		Limits: Limits{MaxPendingCalls: 1},
		PublishProvider: func(_ context.Context, provider Provider) (func() error, error) {
			published <- provider
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	contract := Contract{Protocol: ContractProtocol, Methods: []Method{method}}
	opened := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "open_provider", Contract: contract})
	binder, err := NewLocalBinder(LocalBinderOptions{}, <-published)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(context.Background(), BindRequest{Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, callErr := routes.Call(ctx, Call{Method: method})
		done <- callErr
	}()
	for {
		session.mu.Lock()
		provider := session.providers[opened.Lease]
		session.mu.Unlock()
		pending := false
		if provider != nil {
			provider.mu.Lock()
			pending = len(provider.pending) == 1
			provider.mu.Unlock()
		}
		if pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) && codeOf(err) != CodeCanceled {
		t.Fatalf("canceled call = %v", err)
	}
	event := invokeFFI(context.Background(), t, session, ffiRequest{Operation: "accept", Lease: opened.Lease})
	if event.Operation != "cancel" || event.RequestID == 0 {
		t.Fatalf("cancel event = %#v", event)
	}
}

func TestFFIProviderCanceledPublicationDoesNotCommitLease(t *testing.T) {
	method := Method{ID: "example.publish::Service.Call", Service: "example.publish::Service", Name: "Call", ContractHash: testContractHash}
	var releases atomic.Int32
	session, err := newFFISession(sessionConfig{PublishProvider: func(ctx context.Context, _ Provider) (func() error, error) {
		<-ctx.Done()
		return func() error { releases.Add(1); return nil }, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	response := invokeFFI(context.Background(), t, session, ffiRequest{
		Operation: "open_provider", Contract: Contract{Protocol: ContractProtocol, Methods: []Method{method}}, TimeoutNanos: int64(time.Millisecond),
	})
	if response.Code != CodeDeadlineExceeded || releases.Load() != 1 {
		t.Fatalf("canceled publication = %#v, releases=%d", response, releases.Load())
	}
	session.mu.Lock()
	providers, reservations := len(session.providers), session.reservations
	session.mu.Unlock()
	if providers != 0 || reservations != 0 {
		t.Fatalf("canceled publication committed state: providers=%d reservations=%d", providers, reservations)
	}
}

func TestFFIProviderCommittedResponseWinsLaterCancellation(t *testing.T) {
	payload, err := EncodeValues([]Value{{Type: "string", Data: "ready"}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	call := &ffiProviderCall{result: make(chan ffiProviderOutcome, 1), queued: true, accepted: true}
	provider := &ffiProvider{limits: normalizeLimits(Limits{}), pending: map[uint64]*ffiProviderCall{1: call}}
	if err := provider.respond(1, payload, "", ""); err != nil {
		t.Fatal(err)
	}
	if provider.cancelPending(1) {
		t.Fatal("cancellation replaced an already committed response")
	}
	outcome := <-call.result
	if outcome.err != nil || len(outcome.values) != 1 || outcome.values[0].Data != "ready" {
		t.Fatalf("response outcome = %#v", outcome)
	}
	provider.removePending(1)
	if len(provider.pending) != 0 {
		t.Fatalf("response retained pending state: %#v", provider.pending)
	}
}

func TestFFIProviderCancellationQueueIsDemandAllocated(t *testing.T) {
	provider := &ffiProvider{
		pending: make(map[uint64]*ffiProviderCall),
		cancel:  make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	const calls = 128
	for id := uint64(1); id <= calls; id++ {
		provider.pending[id] = &ffiProviderCall{queued: true, accepted: true}
		if !provider.cancelPending(id) {
			t.Fatalf("cancel pending call %d", id)
		}
	}
	for range calls {
		event, err := provider.accept(context.Background())
		if err != nil || event.kind != "cancel" {
			t.Fatalf("accept cancellation = %#v, %v", event, err)
		}
	}
	if provider.canceled != nil || len(provider.pending) != 0 {
		t.Fatalf("cancellation queue retained state: canceled=%d pending=%d", len(provider.canceled), len(provider.pending))
	}
}

func TestFFIGuestResourceReleaseCanRetryAfterTimeout(t *testing.T) {
	provider := &ffiProvider{events: make(chan ffiProviderEvent, 1), done: make(chan struct{})}
	provider.events <- ffiProviderEvent{kind: "occupied"}
	resource := &guestProviderResource{provider: provider, ref: ResourceRef{Epoch: 1, ObjectID: 1, TypeHash: testContractHash}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resource.Close(ctx); codeOf(err) != CodeCanceled {
		t.Fatalf("first close = %v", err)
	}
	<-provider.events
	if err := resource.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	event := <-provider.events
	if event.kind != "release" || event.receiver == nil || event.receiver.ObjectID != 1 {
		t.Fatalf("release event = %#v", event)
	}
	if err := resource.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func invokeFFI(ctx context.Context, t *testing.T, session *ffiSession, request ffiRequest) ffiResponse {
	t.Helper()
	request.Version = FFIProtocol
	result := make(chan ffi.Result, 1)
	call, err := session.Start(ctx, ffi.Request{Route: FFIRoute, Payload: encodeFFIRequest(request)}, func(value ffi.Result) { result <- value })
	if err != nil {
		t.Fatal(err)
	}
	defer call.Cancel()
	completed := <-result
	if completed.Err != nil {
		t.Fatal(completed.Err)
	}
	response, err := decodeFFIResponse(completed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
