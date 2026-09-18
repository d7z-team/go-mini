package router

import (
	"context"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
)

func TestMountedLeaseCloseReclaimsDownstreamResources(t *testing.T) {
	method := testMethod()
	resource := &mountedTestResource{}
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(ctx context.Context, _ []rpc.Value) ([]rpc.Value, error) {
		value, err := rpc.Export(ctx, resource, contractHash)
		return []rpc.Value{value}, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(t.Context(), rpc.BindRequest{Contract: provider.RPCContract()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = routes.Shutdown(context.Background()) })
	result, err := routes.Call(t.Context(), rpc.Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Accept(t.Context()); err != nil {
		t.Fatal(err)
	}
	lease := &mountedProviderLease{routes: routes}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if resource.closes.Load() != 1 {
		t.Fatal("proxy lease returned while downstream resource remained owned")
	}
}
