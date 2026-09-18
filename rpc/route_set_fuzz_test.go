package rpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type fuzzRouteResource struct {
	closes atomic.Int32
}

func (r *fuzzRouteResource) Invoke(context.Context, string, []Value) ([]Value, error) {
	if r.closes.Load() != 0 {
		return nil, StatusError{Code: CodeNotFound, Message: "rpc resource is closed"}
	}
	return []Value{{Type: "string", Data: "value"}}, nil
}

func (r *fuzzRouteResource) Close(context.Context) error {
	if r.closes.Add(1) != 1 {
		return errors.New("rpc resource closed more than once")
	}
	return nil
}

func FuzzRouteSetResourceLifecycle(f *testing.F) {
	f.Add([]byte{0, 1, 3, 5, 4, 0, 2})
	f.Add([]byte{0, 6, 3, 4, 5})
	f.Add([]byte{0, 0, 1, 2, 5, 4})
	f.Add([]byte{0, 7, 0, 7, 0, 1, 4})

	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			t.Skip()
		}
		open := Method{ID: "fuzz/files::Files.Open", Service: "fuzz/files::Files", Name: "Open", ContractHash: testContractHash}
		read := Method{ID: "fuzz/files::File.Read", Service: "fuzz/files::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
		var resources []*fuzzRouteResource
		provider, err := NewProvider(MethodBinding{Method: open, Invoke: func(ctx context.Context, _ []Value) ([]Value, error) {
			resource := &fuzzRouteResource{}
			resources = append(resources, resource)
			value, err := Export(ctx, resource, testResourceHash)
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
		var pending []*Result
		var accepted []ResourceRef

		for _, operation := range operations {
			switch operation % 8 {
			case 7:
				if len(pending) != 0 {
					result := pending[len(pending)-1]
					pending = pending[:len(pending)-1]
					if err := result.Accept(context.Background()); err != nil {
						t.Fatal(err)
					}
					if err := result.release(); err != nil {
						t.Fatal(err)
					}
				}
			case 0:
				result, callErr := routes.Call(context.Background(), Call{Method: open})
				if callErr == nil {
					pending = append(pending, result)
				}
			case 1:
				if len(pending) != 0 {
					result := pending[len(pending)-1]
					pending = pending[:len(pending)-1]
					if err := result.Accept(context.Background()); err != nil {
						t.Fatal(err)
					}
					if len(result.Values) != 1 || result.Values[0].Resource == nil {
						t.Fatalf("accepted resource result = %#v", result.Values)
					}
					accepted = append(accepted, *result.Values[0].Resource)
				}
			case 2:
				if len(pending) != 0 {
					result := pending[len(pending)-1]
					pending = pending[:len(pending)-1]
					if err := result.Discard(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			case 3:
				if len(accepted) != 0 {
					ref := accepted[len(accepted)-1]
					result, callErr := routes.Call(context.Background(), Call{Method: read, Receiver: &ref})
					if callErr != nil {
						t.Fatal(callErr)
					}
					if err := result.Discard(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			case 4:
				if len(accepted) != 0 {
					ref := accepted[len(accepted)-1]
					accepted = accepted[:len(accepted)-1]
					if err := routes.Drop(context.Background(), ref); err != nil {
						t.Fatal(err)
					}
				}
			case 5:
				if err := routes.Close(); err != nil {
					t.Fatal(err)
				}
			case 6:
				if len(pending) != 0 {
					result := pending[len(pending)-1]
					pending = pending[:len(pending)-1]
					if err := result.Accept(context.Background()); err != nil {
						t.Fatal(err)
					}
					if err := result.Discard(context.Background()); codeOf(err) != CodeFailedPrecondition {
						t.Fatalf("opposite decision = %v", err)
					}
					accepted = append(accepted, *result.Values[0].Resource)
				}
			}
		}
		if len(routes.resourceOrder) != len(accepted) {
			t.Fatalf("resource order retains historical entries: %d, active %d", len(routes.resourceOrder), len(accepted))
		}
		for _, result := range pending {
			if err := result.Discard(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		for index := len(accepted) - 1; index >= 0; index-- {
			if err := routes.Drop(context.Background(), accepted[index]); err != nil {
				t.Fatal(err)
			}
		}
		if err := routes.Close(); err != nil {
			t.Fatal(err)
		}
		if err := routes.Close(); err != nil {
			t.Fatal(err)
		}
		for index, resource := range resources {
			if closes := resource.closes.Load(); closes != 1 {
				t.Fatalf("resource %d closed %d times", index, closes)
			}
		}
		routes.mu.Lock()
		pendingCalls, pendingResults := routes.pendingCalls, routes.pendingResults
		resourceCount := len(routes.resources) + len(routes.provisionalResources) + len(routes.remoteResources) + len(routes.pendingRemoteResources)
		closed := routes.state == routeSetClosed
		routes.mu.Unlock()
		if pendingCalls != 0 || pendingResults != 0 || resourceCount != 0 || !closed {
			t.Fatalf("route set leaked state: calls=%d results=%d resources=%d closed=%v", pendingCalls, pendingResults, resourceCount, closed)
		}
	})
}
