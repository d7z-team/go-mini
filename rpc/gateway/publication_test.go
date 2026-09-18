package gateway

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

type stagedPublicationHandler struct {
	snapshot       PublicationSnapshot
	ready          chan struct{}
	release        chan struct{}
	published      chan struct{}
	readyOnce      sync.Once
	publishedOnce  sync.Once
	mu             sync.Mutex
	publishedCalls int
	failPublished  bool
}

func (handler *stagedPublicationHandler) Snapshot(context.Context) (MiniGoGatewayControlPublicationSnapshot, error) {
	return publicationSnapshotToWire(handler.snapshot), nil
}

func (*stagedPublicationHandler) Resolve(context.Context, string, string) (MiniGoGatewayControlContractBundle, error) {
	return MiniGoGatewayControlContractBundle{}, rpc.StatusError{Code: rpc.CodeNotFound, Message: "contract is not published"}
}

func (handler *stagedPublicationHandler) Ready(ctx context.Context, snapshotID string) error {
	if snapshotID != handler.snapshot.ID {
		return rpc.StatusError{Code: rpc.CodeProtocol, Message: "snapshot identity mismatch"}
	}
	handler.readyOnce.Do(func() { close(handler.ready) })
	select {
	case <-handler.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (handler *stagedPublicationHandler) Published(_ context.Context, snapshotID string) error {
	if snapshotID != handler.snapshot.ID {
		return rpc.StatusError{Code: rpc.CodeProtocol, Message: "snapshot identity mismatch"}
	}
	handler.mu.Lock()
	handler.publishedCalls++
	call := handler.publishedCalls
	handler.mu.Unlock()
	if handler.failPublished && call == 1 {
		return rpc.StatusError{Code: rpc.CodeUnavailable, Message: "publication acknowledgement was lost"}
	}
	handler.publishedOnce.Do(func() { close(handler.published) })
	return nil
}

func TestRemotePublicationIsInvisibleUntilProviderReady(t *testing.T) {
	method := rpc.Method{ID: "example/staged::Service.Get", Service: "example/staged::Service", Name: "Get", ContractHash: testContractHash}
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
		return []rpc.Value{{Type: "string", Data: "ready"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := normalizePublicationSnapshot(PublicationSnapshot{
		Protocol: PublicationProtocol, ProcessID: "staged", Generation: 1,
		Providers: []PublishedProvider{{ID: "service", Contract: provider.RPCContract(), Options: rpcrouter.RegistrationOptions{Name: "service"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := &stagedPublicationHandler{
		snapshot: snapshot, ready: make(chan struct{}), release: make(chan struct{}), published: make(chan struct{}), failPublished: true,
	}
	control, err := NewMiniGoGatewayControlPublicationProvider(handler)
	if err != nil {
		t.Fatal(err)
	}
	local := rpcrouter.New(rpcrouter.Options{})
	publication, err := local.Publish([]rpcrouter.ProviderEntry{{Provider: provider, Options: rpcrouter.RegistrationOptions{Name: "service"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Register(control, rpcrouter.RegistrationOptions{Name: "control"}); err != nil {
		t.Fatal(err)
	}

	routed := rpcrouter.New(rpcrouter.Options{})
	registry, err := NewPublicationRegistry(routed, PublicationRegistryOptions{ControlTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(routed, ServerOptions{Publications: registry})
	if err != nil {
		t.Fatal(err)
	}
	address := "ws+unix://" + filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServer := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(serveCtx, listener, server) }()
	endpoint, err := Dial(context.Background(), address, DialOptions{Path: publicationWebSocketPath, Services: rpc.EndpointServices{Binder: local}})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	select {
	case <-handler.ready:
	case <-time.After(time.Second):
		stopServer()
		t.Fatal("Gateway did not request provider readiness")
	}
	if _, err := routed.Bind(context.Background(), rpc.BindRequest{Contract: provider.RPCContract()}); err == nil {
		stopServer()
		t.Fatal("candidate publication became visible before Ready completed")
	}
	close(handler.release)
	select {
	case <-handler.published:
	case <-time.After(time.Second):
		stopServer()
		t.Fatal("Gateway did not acknowledge visible publication")
	}
	handler.mu.Lock()
	publishedCalls := handler.publishedCalls
	handler.mu.Unlock()
	if publishedCalls < 2 {
		stopServer()
		t.Fatalf("Published acknowledgement calls = %d, want retry", publishedCalls)
	}
	routes, err := routed.Bind(context.Background(), rpc.BindRequest{Contract: provider.RPCContract()})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publication.ForceClose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := local.ForceShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopServer()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if err := routed.ForceShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRemotePublicationUpdatesCatalogAndRoutesUntilDisconnect(t *testing.T) {
	method := rpc.Method{ID: "example/remote::Greeter.Hello", Service: "example/remote::Greeter", Name: "Hello", ContractHash: testContractHash}
	bundle, err := rpcrouter.NewMRPCBundle("example/remote", []rpcrouter.MRPCFile{{Path: "remote.mrpc", Text: "syntax = \"mrpc/v2\"; package remote; service Greeter { Hello() (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	base, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
		return []rpc.Value{{Type: "string", Data: "remote"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	provider := base

	routed := rpcrouter.New(rpcrouter.Options{})
	catalog, err := NewCatalogProvider(routed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routed.Register(catalog, rpcrouter.RegistrationOptions{Name: "catalog"}); err != nil {
		t.Fatal(err)
	}
	publications, err := NewPublicationRegistry(routed, PublicationRegistryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(routed, ServerOptions{Publications: publications})
	if err != nil {
		t.Fatal(err)
	}
	address := "ws+unix://" + filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServer := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(serveCtx, listener, server) }()

	consumer, err := Dial(context.Background(), address, DialOptions{})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	catalogClient, err := NewClient(context.Background(), consumer)
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	initial, err := catalogClient.Snapshot(context.Background())
	if err != nil {
		stopServer()
		t.Fatal(err)
	}

	publisher, err := NewPublisher("process", 1, []PublicationProvider{{Provider: provider, Bundles: []rpcrouter.MRPCBundle{bundle}}})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	local := rpcrouter.New(rpcrouter.Options{})
	publication, err := local.Publish([]rpcrouter.ProviderEntry{{Provider: provider, Options: rpcrouter.RegistrationOptions{Name: "provider"}}})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	if _, err := local.Register(publisher.Provider(), rpcrouter.RegistrationOptions{Name: "publication"}); err != nil {
		stopServer()
		t.Fatal(err)
	}
	publishEndpoint, err := Dial(context.Background(), address, DialOptions{Path: publicationWebSocketPath, Services: rpc.EndpointServices{Binder: local}})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	publishCtx, cancelPublish := context.WithTimeout(context.Background(), time.Second)
	defer cancelPublish()
	if err := publisher.WaitPublished(publishCtx); err != nil {
		stopServer()
		t.Fatal(err)
	}

	watchCtx, cancelWatch := context.WithTimeout(context.Background(), time.Second)
	published, err := catalogClient.Watch(watchCtx, initial)
	cancelWatch()
	if err != nil || len(published.References) != 1 || published.References[0].Hash != bundle.Hash {
		stopServer()
		t.Fatalf("published catalog = %#v, err=%v", published, err)
	}
	routes, err := consumer.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	result, err := routes.Call(context.Background(), rpc.Call{Method: method})
	if err != nil {
		stopServer()
		t.Fatal(err)
	}
	if len(result.Values) != 1 || result.Values[0].Data != "remote" {
		stopServer()
		t.Fatalf("remote result = %#v", result.Values)
	}
	if err := result.Accept(context.Background()); err != nil {
		stopServer()
		t.Fatal(err)
	}
	if err := routes.Close(); err != nil {
		stopServer()
		t.Fatal(err)
	}
	if err := publishEndpoint.Close(); err != nil {
		stopServer()
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(routed.Revision().References) != 0 {
		if time.Now().After(deadline) {
			stopServer()
			t.Fatal("disconnected publication remained in the Gateway catalog")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := consumer.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}); err == nil {
		stopServer()
		t.Fatal("disconnected publication remained routable")
	}

	if err := catalogClient.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publication.ForceClose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := local.ForceShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopServer()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if err := routed.ForceShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type identifiedTestProvider struct {
	rpc.Provider
	id string
}

func (provider identifiedTestProvider) RPCProviderID() string { return provider.id }

func TestPublisherUsesStableProviderIdentity(t *testing.T) {
	method := rpc.Method{ID: "example/identity::Service.Get", Service: "example/identity::Service", Name: "Get", ContractHash: testContractHash}
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
		return []rpc.Value{{Type: "string", Data: "value"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := NewPublisher("process", 1, []PublicationProvider{{Provider: identifiedTestProvider{Provider: provider, id: "guest.greeter"}}})
	if err != nil {
		t.Fatal(err)
	}
	local := rpcrouter.New(rpcrouter.Options{})
	if _, err := local.Register(publisher.Provider(), rpcrouter.RegistrationOptions{Name: "publication"}); err != nil {
		t.Fatal(err)
	}
	client, err := newPublicationClient(context.Background(), local)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Providers) != 1 || snapshot.Providers[0].ID != "guest.greeter" || snapshot.Providers[0].Options.Name != "guest.greeter" {
		t.Fatalf("publisher identity = %#v", snapshot.Providers)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := local.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationGenerationIdentity(t *testing.T) {
	current := &remotePublication{generation: 4, snapshotID: "current"}
	tests := []struct {
		name     string
		current  *remotePublication
		incoming PublicationSnapshot
		wantErr  bool
		identity string
	}{
		{name: "first", incoming: PublicationSnapshot{Generation: 1, ID: "first"}},
		{name: "same generation reconnect", current: current, incoming: PublicationSnapshot{Generation: 4, ID: "current"}},
		{name: "next generation", current: current, incoming: PublicationSnapshot{Generation: 5, ID: "next"}},
		{name: "stale active", current: current, incoming: PublicationSnapshot{Generation: 3, ID: "stale"}, wantErr: true},
		{name: "same generation changed content", current: current, incoming: PublicationSnapshot{Generation: 4, ID: "different"}, wantErr: true},
		{name: "authenticated identity changed", current: &remotePublication{generation: 4, snapshotID: "current", peerIdentity: "owner"}, incoming: PublicationSnapshot{Generation: 5, ID: "next"}, identity: "other", wantErr: true},
		{name: "authenticated reconnect", current: &remotePublication{generation: 4, snapshotID: "current", peerIdentity: "owner"}, incoming: PublicationSnapshot{Generation: 4, ID: "current"}, identity: "owner"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePublicationIdentity(test.current, test.incoming, test.identity)
			if (err != nil) != test.wantErr {
				t.Fatalf("validatePublicationIdentity() error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestPublisherRequiresReadyBeforePublished(t *testing.T) {
	publisher := &Publisher{published: make(chan struct{})}
	handler := publicationHandler{snapshot: PublicationSnapshot{ID: "snapshot"}, publisher: publisher}
	if err := handler.Published(context.Background(), "snapshot"); err == nil {
		t.Fatal("Published accepted a candidate publication")
	}
	if err := handler.Ready(context.Background(), "other"); err == nil {
		t.Fatal("Ready accepted a different snapshot")
	}
	if err := handler.Ready(context.Background(), "snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := handler.Ready(context.Background(), "snapshot"); err != nil {
		t.Fatalf("Ready is not idempotent: %v", err)
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelWait()
	if err := publisher.WaitPublished(waitCtx); err == nil {
		t.Fatal("ready publication was reported as visible")
	}
	if err := handler.Published(context.Background(), "snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := handler.Published(context.Background(), "snapshot"); err != nil {
		t.Fatalf("Published is not idempotent: %v", err)
	}
	if err := publisher.WaitPublished(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func FuzzPublicationSnapshotCanonicalRoundTrip(f *testing.F) {
	f.Add("process", uint64(1), "provider")
	f.Add("worker/1", uint64(7), "service.primary")
	f.Fuzz(func(t *testing.T, processID string, generation uint64, providerID string) {
		if len(processID)+len(providerID) > 1024 || generation == 0 {
			t.Skip()
		}
		snapshot := PublicationSnapshot{
			Protocol: PublicationProtocol, ProcessID: processID, Generation: generation,
			Providers: []PublishedProvider{{
				ID: providerID,
				Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{{
					ID: "example/fuzz::Service.Get", Service: "example/fuzz::Service", Name: "Get", ContractHash: testContractHash,
				}}},
			}},
		}
		normalized, err := normalizePublicationSnapshot(snapshot)
		if err != nil {
			return
		}
		decoded, err := publicationSnapshotFromWire(publicationSnapshotToWire(normalized))
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := normalizePublicationSnapshot(decoded)
		if err != nil || roundTrip.ID != normalized.ID {
			t.Fatalf("publication snapshot changed across typed wire: before=%#v after=%#v err=%v", normalized, roundTrip, err)
		}
	})
}
