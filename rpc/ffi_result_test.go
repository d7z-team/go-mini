package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/ffi"
)

func TestFFIResourceReplyOwnership(t *testing.T) {
	for _, operation := range []string{"call", "call_owned"} {
		for _, remote := range []bool{false, true} {
			for _, disposition := range []string{"consume", "discard", "shutdown"} {
				name := "local"
				if remote {
					name = "remote"
				}
				name = operation + "/" + name + "/" + disposition
				t.Run(name, func(t *testing.T) {
					ctx := context.Background()
					method := Method{ID: "example::Files.Open", Service: "example::Files", Name: "Open", ContractHash: testContractHash}
					resource := &countedResource{}
					provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
						value, err := Export(ctx, resource, testResourceHash)
						return []Value{value}, err
					})
					local, err := NewLocalBinder(LocalBinderOptions{}, provider)
					if err != nil {
						t.Fatal(err)
					}
					var binder Binder = local
					if remote {
						binder, _ = openEndpointPair(t, nil, local, EndpointOptions{})
					}
					session, err := newFFISession(sessionConfig{Binder: binder})
					if err != nil {
						t.Fatal(err)
					}
					defer session.Shutdown(ctx)
					id, err := session.open(ctx, testContract(method), BindOptions{})
					if err != nil {
						t.Fatal(err)
					}
					payload, err := EncodeValues(nil, Limits{})
					if err != nil {
						t.Fatal(err)
					}
					result := session.handle(ctx, encodeFFIRequest(ffiRequest{Version: FFIProtocol, Operation: operation, Lease: id, Method: method, Payload: payload}))
					response, err := decodeFFIResponse(result.Payload)
					if err != nil || response.Code != "" || result.Discard == nil {
						t.Fatalf("call = %#v, %v", response, err)
					}
					if disposition == "shutdown" {
						discarded := make(chan struct{})
						go func() { result.Discard(); close(discarded) }()
						if err := session.Shutdown(ctx); err != nil {
							t.Fatal(err)
						}
						<-discarded
					} else if disposition == "discard" {
						result.Discard()
						result.Discard()
						session.workers.Wait()
					} else {
						if operation == "call_owned" {
							if response.RequestID == 0 {
								t.Fatal("pending result has no decision credential")
							}
							accepted := invokeFFI(ctx, t, session, ffiRequest{Operation: "accept_result", Lease: id, RequestID: response.RequestID})
							if accepted.Code != "" {
								t.Fatalf("accept: %+v", accepted)
							}
						}
						if resource.closes.Load() != 0 {
							t.Fatal("consumed result lost its resource")
						}
						values, err := DecodeValues(response.Payload, Limits{})
						if err != nil {
							t.Fatal(err)
						}
						if err := session.leases[id].Drop(ctx, *values[0].Resource); err != nil {
							t.Fatal(err)
						}
					}
					if resource.closes.Load() != 1 {
						t.Fatalf("resource closes = %d", resource.closes.Load())
					}
					if err := session.Shutdown(ctx); err != nil {
						t.Fatal(err)
					}
					if resource.closes.Load() != 1 {
						t.Fatal("shutdown closed the resource twice")
					}
				})
			}
		}
	}
}

func TestFFIPendingResultsHaveBoundedLeaseOwnership(t *testing.T) {
	for name, limits := range map[string]Limits{"count": {MaxPendingResults: 1}, "bytes": {MaxInFlightBytes: 1}} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
			provider := newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) { return nil, nil })
			binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
			if err != nil {
				t.Fatal(err)
			}
			session, err := newFFISession(sessionConfig{Binder: binder, Limits: limits})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			first, err := session.open(ctx, testContract(method), BindOptions{})
			if err != nil {
				t.Fatal(err)
			}
			second, err := session.open(ctx, testContract(method), BindOptions{})
			if err != nil {
				t.Fatal(err)
			}
			payload, err := EncodeValues(nil, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			request := ffiRequest{Operation: "call_owned", Lease: first, Method: method, Payload: payload}
			result := invokeFFI(ctx, t, session, request)
			if result.Code != "" || result.RequestID == 0 {
				t.Fatalf("call: %+v", result)
			}
			if full := invokeFFI(ctx, t, session, request); full.Code != CodeResourceExhausted {
				t.Fatalf("pending limit: %+v", full)
			}
			foreign := invokeFFI(ctx, t, session, ffiRequest{Operation: "discard_result", Lease: second, RequestID: result.RequestID})
			if foreign.Code != CodeInvalidArgument {
				t.Fatalf("foreign result: %+v", foreign)
			}
			if err := session.closeLease(ctx, first); err != nil {
				t.Fatal(err)
			}
			request.Lease = second
			result = invokeFFI(ctx, t, session, request)
			if result.Code != "" {
				t.Fatalf("closed binding retained result capacity: %+v", result)
			}
			for range 2 {
				if response := invokeFFI(ctx, t, session, ffiRequest{Operation: "discard_result", Lease: second, RequestID: result.RequestID}); response.Code != "" {
					t.Fatalf("discard: %+v", response)
				}
			}
		})
	}
}

func TestFFIDiscardedOpenReleasesCommittedLease(t *testing.T) {
	for _, operation := range []string{"open", "open_provider"} {
		t.Run(operation, func(t *testing.T) {
			method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
			lease := &notifyingProviderLease{done: make(chan struct{})}
			provider := localBinderProvider{contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) { return lease, nil }}
			binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
			if err != nil {
				t.Fatal(err)
			}
			session, err := newFFISession(sessionConfig{Binder: binder, PublishProvider: func(context.Context, Provider) (func() error, error) { return lease.Close, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Shutdown(context.Background())
			request := ffiRequest{Version: FFIProtocol, Operation: operation, Contract: testContract(method)}
			resultCh := make(chan ffi.Result, 1)
			call, err := session.Start(context.Background(), ffi.Request{Route: FFIRoute, Payload: encodeFFIRequest(request)}, func(result ffi.Result) { resultCh <- result })
			if err != nil {
				t.Fatal(err)
			}
			result := <-resultCh
			call.Cancel()
			response, err := decodeFFIResponse(result.Payload)
			if err != nil || response.Code != "" || response.Lease == 0 || result.Discard == nil {
				t.Fatalf("open = %#v, %v", response, err)
			}
			result.Discard()
			result.Discard()
			select {
			case <-lease.done:
			case <-time.After(time.Second):
				t.Fatal("discarded response retained its committed lease")
			}
			if err := session.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if lease.closes.Load() != 1 || session.reservations != 0 || len(session.leases) != 0 || len(session.providers) != 0 {
				t.Fatal("discard and shutdown did not release binding exactly once")
			}
		})
	}
}
