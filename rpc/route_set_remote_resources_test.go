package rpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRemoteRouteSetPublishesResourcesOnAccept(t *testing.T) {
	method := Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: testContractHash}
	refs := []ResourceRef{
		{Epoch: 7, ObjectID: 1, TypeHash: testResourceHash},
		{Epoch: 7, ObjectID: 2, TypeHash: testResourceHash},
	}
	routes := newRemoteRouteSet(7, normalizeLimits(Limits{}), []Method{method}, &remoteBinding{
		call: func(context.Context, Call) (*Result, error) {
			return &Result{Values: []Value{{Type: testResourceHash, Resource: &refs[0]}, {Type: testResourceHash, Resource: &refs[1]}}}, nil
		},
	})
	result, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	routes.mu.Lock()
	if len(routes.remoteResources) != 0 || len(routes.pendingRemoteResources) != 2 {
		t.Fatalf("resources published before Accept: active=%d pending=%d", len(routes.remoteResources), len(routes.pendingRemoteResources))
	}
	routes.mu.Unlock()
	if err := result.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes.mu.Lock()
	defer routes.mu.Unlock()
	if len(routes.remoteResources) != 2 || len(routes.pendingRemoteResources) != 0 || routes.pendingResults != 0 {
		t.Fatalf("accepted resource state: active=%d pending=%d results=%d", len(routes.remoteResources), len(routes.pendingRemoteResources), routes.pendingResults)
	}
}

func TestRemoteResultAliasesReleaseOnlyNewResources(t *testing.T) {
	method := Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: testContractHash}
	old := ResourceRef{Epoch: 7, ObjectID: 1, TypeHash: testResourceHash}
	fresh := ResourceRef{Epoch: 7, ObjectID: 2, TypeHash: testResourceHash}
	var dropped []uint64
	routes := newRemoteRouteSet(7, normalizeLimits(Limits{}), []Method{method}, &remoteBinding{
		call: func(context.Context, Call) (*Result, error) {
			return &Result{Values: []Value{
				{Type: old.TypeHash, Resource: &old},
				{Type: fresh.TypeHash, Resource: &fresh},
				{Type: fresh.TypeHash, Resource: &fresh},
				{Type: old.TypeHash, Resource: &old},
			}}, nil
		},
		drop:  func(_ context.Context, ref ResourceRef) error { dropped = append(dropped, ref.ObjectID); return nil },
		close: func() error { return nil },
	})
	t.Cleanup(func() { _ = routes.Shutdown(context.Background()) })
	routes.remoteResources[old.ObjectID] = old
	result, err := routes.Call(t.Context(), Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Accept(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(routes.remoteResources) != 2 {
		t.Fatal("aliases changed resource accounting")
	}
	if err := result.release(); err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 1 || dropped[0] != fresh.ObjectID {
		t.Fatalf("released resources = %v", dropped)
	}
	if routes.remoteResources[old.ObjectID] != old {
		t.Fatal("accepted alias lost existing resource")
	}
}

func TestRemoteRouteSetRejectsInvalidResourceResults(t *testing.T) {
	method := Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: testContractHash}
	tests := []struct {
		name   string
		values func(*RouteSet) []Value
	}{
		{name: "different route-set epoch", values: func(*RouteSet) []Value {
			ref := ResourceRef{Epoch: 8, ObjectID: 1, TypeHash: testResourceHash}
			return []Value{{Type: testResourceHash, Resource: &ref}}
		}},
		{name: "conflicting id", values: func(*RouteSet) []Value {
			first := ResourceRef{Epoch: 7, ObjectID: 1, TypeHash: testResourceHash}
			second := first
			second.TypeHash = testContractHash
			return []Value{{Type: first.TypeHash, Resource: &first}, {Type: second.TypeHash, Resource: &second}}
		}},
		{name: "active collision", values: func(routes *RouteSet) []Value {
			ref := ResourceRef{Epoch: 7, ObjectID: 1, TypeHash: testResourceHash}
			routes.remoteResources[ref.ObjectID] = ref
			ref.TypeHash = testContractHash
			return []Value{{Type: ref.TypeHash, Resource: &ref}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var discarded atomic.Int32
			var values []Value
			routes := newRemoteRouteSet(7, normalizeLimits(Limits{}), []Method{method}, &remoteBinding{
				call: func(context.Context, Call) (*Result, error) {
					return &Result{Values: values, accept: func(accept bool) error {
						if !accept {
							discarded.Add(1)
						}
						return nil
					}}, nil
				},
			})
			values = test.values(routes)
			if _, err := routes.Call(context.Background(), Call{Method: method}); codeOf(err) != CodeProtocol {
				t.Fatalf("Call() error = %v", err)
			}
			if discarded.Load() != 1 {
				t.Fatalf("discard decisions = %d", discarded.Load())
			}
			routes.mu.Lock()
			if len(routes.pendingRemoteResources) != 0 || routes.pendingResults != 0 {
				t.Fatalf("invalid result changed pending state: resources=%d results=%d", len(routes.pendingRemoteResources), routes.pendingResults)
			}
			routes.mu.Unlock()
		})
	}
}

func TestRemoteRouteSetValidatesArgumentsAndDropsOnce(t *testing.T) {
	method := Method{ID: "example/files::Files.Use", Service: "example/files::Files", Name: "Use", ContractHash: testContractHash}
	var calls atomic.Int32
	var drops atomic.Int32
	routes := newRemoteRouteSet(7, normalizeLimits(Limits{}), []Method{method}, &remoteBinding{
		call: func(context.Context, Call) (*Result, error) {
			calls.Add(1)
			return &Result{}, nil
		},
		drop: func(context.Context, ResourceRef) error {
			drops.Add(1)
			return nil
		},
	})
	ref := ResourceRef{Epoch: 7, ObjectID: 1, TypeHash: testResourceHash}
	if _, err := routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: testResourceHash, Resource: &ref}}}); codeOf(err) != CodeInvalidArgument {
		t.Fatalf("unowned resource argument error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("unowned resource argument reached the transport")
	}
	routes.remoteResources[ref.ObjectID] = ref
	result, err := routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: testResourceHash, Resource: &ref}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Discard(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- routes.Drop(context.Background(), ref)
		}()
	}
	wait.Wait()
	close(errors)
	var successes int
	for err := range errors {
		if err == nil {
			successes++
		} else if codeOf(err) != CodeNotFound {
			t.Fatalf("concurrent Drop() error = %v", err)
		}
	}
	// Overlapping callers share the attempt; a caller arriving after removal
	// sees not_found. Both schedules must issue exactly one transport drop.
	if successes < 1 || drops.Load() != 1 {
		t.Fatalf("Drop() successes=%d transport calls=%d", successes, drops.Load())
	}
}
