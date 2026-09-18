package rpc

import (
	"context"
	"sync"
	"testing"
)

func TestResourceHandlesShareLifetimeAndDiscardInvalidates(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "discard", true: "accept"}[accept], func(t *testing.T) {
			ctx := context.Background()
			method := Method{ID: "example::Files.Open", Service: "example::Files", Name: "Open", ContractHash: testContractHash}
			resource := &countedResource{}
			provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
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
			result, err := routes.Call(ctx, Call{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			ref := *result.Values[0].Resource
			first, err := routes.BindResource(ref)
			if err != nil {
				t.Fatal(err)
			}
			second, err := routes.BindResource(ref)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := second.Reference(ctx, &RouteSet{}); err == nil {
				t.Fatal("cross-binding handle accepted")
			}
			if accept {
				if err := result.Accept(context.Background()); err != nil {
					t.Fatal(err)
				}
				var wg sync.WaitGroup
				for _, handle := range []*ResourceHandle{first, second} {
					wg.Go(func() {
						if err := handle.Close(ctx); err != nil {
							t.Errorf("close: %v", err)
						}
					})
				}
				wg.Wait()
			} else if err := result.Discard(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := first.Reference(ctx, routes); err == nil {
				t.Fatal("closed handle remained usable")
			}
			if _, err := second.Reference(ctx, routes); err == nil {
				t.Fatal("alias remained usable")
			}
			if _, err := routes.BindResource(ref); err == nil {
				t.Fatal("released resource was rebound")
			}
			if resource.closes.Load() != 1 || len(routes.handles) != 0 {
				t.Fatal("resource lifetime did not close exactly once")
			}
		})
	}
}
