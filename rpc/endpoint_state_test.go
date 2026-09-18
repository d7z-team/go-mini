package rpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestEndpointLimitsUndecidedResults(t *testing.T) {
	method := Method{ID: "example/results::Service.Value", Service: "example/results::Service", Name: "Value", ContractHash: testContractHash}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		return []Value{{Type: "string", Data: "value"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	client, _ := openEndpointPair(t, nil, gateway, EndpointOptions{Limits: Limits{MaxPendingResults: 1}})
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	first, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routes.Call(context.Background(), Call{Method: method}); codeOf(err) != CodeResourceExhausted {
		t.Fatalf("second call = %v", err)
	}
	if err := first.Discard(context.Background()); err != nil {
		t.Fatal(err)
	}
	third, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatalf("call after result decision: %v", err)
	}
	_ = third.Discard(context.Background())
}

func TestEndpointDecisionRequiresBindingIdentity(t *testing.T) {
	method := Method{ID: "example/decision::Service.Value", Service: "example/decision::Service", Name: "Value", ContractHash: testContractHash}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		return []Value{{Type: "string", Data: "value"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	client, server := openEndpointPair(t, nil, gateway, EndpointOptions{})
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	result, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	var operationID, binding uint64
	for id, entry := range server.results {
		operationID, binding = id, entry.binding
	}
	server.mu.Unlock()
	if _, err := client.request(context.Background(), endpointFrame{Kind: "decision", Binding: binding + 1, TargetID: operationID}); codeOf(err) != CodeNotFound {
		t.Fatalf("decision with wrong binding = %v", err)
	}
	if err := result.Discard(context.Background()); err != nil {
		t.Fatalf("correct decision after rejected decision: %v", err)
	}
}

func TestEndpointControlRequestBypassesDataSaturation(t *testing.T) {
	method := Method{ID: "example/control::Service.Value", Service: "example/control::Service", Name: "Value", ContractHash: testContractHash}
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var calls atomic.Int32
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		if calls.Add(1) == 2 {
			close(secondStarted)
			<-releaseSecond
		}
		return []Value{{Type: "string", Data: "value"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	client, _ := openEndpointPair(t, nil, gateway, EndpointOptions{Limits: Limits{MaxPendingCalls: 1}})
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	first, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	type callResult struct {
		result *Result
		err    error
	}
	secondDone := make(chan callResult, 1)
	go func() {
		result, callErr := routes.Call(context.Background(), Call{Method: method})
		secondDone <- callResult{result: result, err: callErr}
	}()
	select {
	case <-secondStarted:
	case outcome := <-secondDone:
		t.Fatalf("second call finished before handler started: %v", outcome.err)
	}
	if err := first.Discard(context.Background()); err != nil {
		t.Fatalf("control request blocked by active call: %v", err)
	}
	close(releaseSecond)
	if outcome := <-secondDone; outcome.result == nil {
		t.Fatalf("second call failed: %v", outcome.err)
	} else {
		_ = outcome.result.Discard(context.Background())
	}
}

func TestEndpointRejectsRepeatedRequestID(t *testing.T) {
	method := Method{ID: "example/id::Service.Value", Service: "example/id::Service", Name: "Value", ContractHash: testContractHash}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) { return nil, nil })); err != nil {
		t.Fatal(err)
	}
	client, server := openEndpointPair(t, nil, gateway, EndpointOptions{})
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	if err := client.write(endpointFrame{Kind: "close", Origin: client.origin, ID: 1, Binding: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("server accepted a repeated request id")
	}
	server.mu.Lock()
	closeErr := server.closeErr
	server.mu.Unlock()
	if codeOf(closeErr) != CodeProtocol {
		t.Fatalf("repeated request id error = %v", closeErr)
	}
}

func TestEndpointRejectsExhaustedRequestID(t *testing.T) {
	method := Method{ID: "example/id::Service.Value", Service: "example/id::Service", Name: "Value", ContractHash: testContractHash}
	client, _ := openEndpointPair(t, nil, nil, EndpointOptions{})
	if err := client.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	client.nextID = ^uint64(0)
	client.mu.Unlock()
	if _, err := client.Bind(context.Background(), testBindRequest(method)); codeOf(err) != CodeResourceExhausted {
		t.Fatalf("exhausted request id error = %v", err)
	}
}

func TestEndpointWireCloseAbortsBindingResources(t *testing.T) {
	method := Method{ID: "example/resource::Service.Open", Service: "example/resource::Service", Name: "Open", ContractHash: testContractHash}
	resource := &countedResource{}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
		value, err := Export(ctx, resource, testResourceHash)
		return []Value{value}, err
	})); err != nil {
		t.Fatal(err)
	}
	client, _ := openEndpointPair(t, nil, gateway, EndpointOptions{})
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	result, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	var binding uint64
	for id := range client.outbound {
		binding = id
	}
	client.mu.Unlock()
	if _, err := client.request(context.Background(), endpointFrame{Kind: "close", Binding: binding}); err != nil {
		t.Fatal(err)
	}
	if resource.closes.Load() != 1 {
		t.Fatalf("resource closes = %d", resource.closes.Load())
	}
}

func TestEndpointDisconnectInvalidatesOutboundRoutes(t *testing.T) {
	method := Method{ID: "example/disconnect::Service.Value", Service: "example/disconnect::Service", Name: "Value", ContractHash: testContractHash}
	gateway := newTestBinder(testBinderOptions{})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		return []Value{{Type: "string", Data: "value"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	client, server := openEndpointPair(t, nil, gateway, EndpointOptions{})
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatalf("bind: %v (client=%v server=%v)", err, client.endpointError(), server.endpointError())
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = routes.Call(context.Background(), Call{Method: method})
	code, _ := CodeOf(err)
	if err == nil || code != CodeUnavailable {
		t.Fatalf("call after disconnect = %v", err)
	}
	_ = client.Close()
}

func TestEndpointSuppliesAuthenticatedPeerToGateway(t *testing.T) {
	method := Method{ID: "example/auth::Service.Value", Service: "example/auth::Service", Name: "Value", ContractHash: testContractHash}
	gateway := newTestBinder(testBinderOptions{Authorizer: func(_ context.Context, peer PeerInfo, _ Contract) error {
		if peer.Identity != "trusted" || peer.Attributes["role"] != "worker" {
			return StatusError{Code: CodeUnavailable, Message: "unauthorized"}
		}
		return nil
	}})
	if err := gateway.Register(newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) {
		return []Value{{Type: "string", Data: "ok"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	client, _ := openEndpointPair(t, nil, gateway, EndpointOptions{Peer: PeerInfo{Identity: "trusted", Attributes: map[string]string{"role": "worker"}}})
	routes, err := client.Bind(context.Background(), BindRequest{
		Contract: testContract(method), Peer: PeerInfo{Identity: "forged", Attributes: map[string]string{"role": "admin"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = routes.Close()
}
