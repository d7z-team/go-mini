package rpc

import (
	"context"
	"slices"
	"testing"
)

func TestRouteSetShutdownPreservesRemainingResourceOrder(t *testing.T) {
	ctx := context.Background()
	var order []int
	var resources []*countedResource
	method := Method{ID: "example::Files.Open", Service: "example::Files", Name: "Open", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
		resource := &countedResource{}
		id := len(resources)
		resources = append(resources, resource)
		value, err := Export(ctx, &orderedResource{Resource: resource, id: id, order: &order}, testResourceHash)
		return []Value{value}, err
	})
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(ctx, testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Shutdown(ctx)
	var refs []ResourceRef
	for range 4 {
		result, err := routes.Call(ctx, Call{Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if err := result.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, *result.Values[0].Resource)
	}
	if err := routes.Drop(ctx, refs[1]); err != nil {
		t.Fatal(err)
	}
	if err := routes.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []int{1, 3, 2, 0}) {
		t.Fatalf("close order = %v", order)
	}
	for _, resource := range resources {
		if resource.closes.Load() != 1 {
			t.Fatalf("resource closed %d times", resource.closes.Load())
		}
	}
}

func TestResultReleasePreservesExistingResources(t *testing.T) {
	ctx := context.Background()
	method := Method{ID: "example::Files.Open", Service: "example::Files", Name: "Open", ContractHash: testContractHash}
	resource := &countedResource{}
	var original Value
	provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
		if original.Resource != nil {
			return []Value{original}, nil
		}
		var err error
		original, err = Export(ctx, resource, testResourceHash)
		return []Value{original}, err
	})
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(ctx, testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Shutdown(ctx)
	for index := range 2 {
		result, err := routes.Call(ctx, Call{Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if err := result.Accept(context.Background()); err != nil {
			t.Fatal(err)
		}
		if index == 1 && result.release != nil {
			if err := result.release(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := routes.Resolve(*original.Resource); err != nil {
		t.Fatalf("existing resource was lost: %v", err)
	}
	if resource.closes.Load() != 0 {
		t.Fatal("returned alias released an existing resource")
	}
}

func TestRouteSetResourceOrderTracksLiveResources(t *testing.T) {
	ctx := context.Background()
	method := Method{ID: "example::Files.Open", Service: "example::Files", Name: "Open", ContractHash: testContractHash}
	var resources []*countedResource
	provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
		resource := &countedResource{}
		resources = append(resources, resource)
		value, err := Export(ctx, resource, testResourceHash)
		return []Value{value}, err
	})
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(ctx, testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Shutdown(ctx)
	for operation := range 100 {
		result, err := routes.Call(ctx, Call{Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if operation%3 == 0 {
			err = result.Discard(context.Background())
		} else {
			err = result.Accept(context.Background())
			if err == nil {
				if operation%3 == 1 {
					err = result.release()
				} else {
					err = routes.Drop(ctx, *result.Values[0].Resource)
				}
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(routes.resources) != 0 || len(routes.resourceOrder) != 0 {
			t.Fatal("released resources retained in ownership index")
		}
	}
	if err := routes.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.closes.Load() != 1 {
			t.Fatalf("resource closes = %d", resource.closes.Load())
		}
	}
}
