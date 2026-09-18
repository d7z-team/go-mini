package gateway

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

const testContractHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func catalogGateway(t *testing.T) (*rpcrouter.Router, rpcrouter.MRPCBundle) {
	t.Helper()
	bundle, err := rpcrouter.NewMRPCBundle("example/catalog", []rpcrouter.MRPCFile{{Path: "catalog.mrpc", Text: "syntax = \"mrpc/v2\"; package catalog; service Value { Get() (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	method := rpc.Method{ID: "example/catalog::Value.Get", Service: "example/catalog::Value", Name: "Get", ContractHash: testContractHash}
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
		return []rpc.Value{{Type: "string", Data: "value"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := rpcrouter.New(rpcrouter.Options{})
	if _, err := gateway.Publish([]rpcrouter.ProviderEntry{{Provider: provider, Bundles: []rpcrouter.MRPCBundle{bundle}}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewCatalogProvider(gateway)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Register(catalog, rpcrouter.RegistrationOptions{Name: "catalog"}); err != nil {
		t.Fatal(err)
	}
	return gateway, bundle
}

func TestServeAndDialCatalog(t *testing.T) {
	gateway, _ := catalogGateway(t)
	address := "ws+unix://" + filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(gateway, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, listener, server) }()
	dialCtx, cancelDial := context.WithCancel(context.Background())
	endpoint, err := Dial(dialCtx, address, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancelDial()
	client, err := NewClient(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	client, err = NewClient(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func FuzzSnapshotIdentity(f *testing.F) {
	f.Add("example/a", testContractHash, "example/b", testContractHash)
	f.Add("example/a", testContractHash, "example/a", testContractHash)
	f.Fuzz(func(t *testing.T, firstPath, firstHash, secondPath, secondHash string) {
		if len(firstPath)+len(firstHash)+len(secondPath)+len(secondHash) > 4096 {
			t.Skip()
		}
		references := []rpcrouter.ContractReference{{ImportPath: firstPath, Hash: firstHash}, {ImportPath: secondPath, Hash: secondHash}}
		first, err := newSnapshot(references)
		if err != nil {
			return
		}
		second, err := newSnapshot([]rpcrouter.ContractReference{references[1], references[0]})
		if err != nil || first.ID != second.ID {
			t.Fatalf("snapshot identity depends on input order: first=%#v second=%#v err=%v", first, second, err)
		}
	})
}
