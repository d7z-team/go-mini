package router

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/rpc"
)

const contractHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testMethod() rpc.Method {
	return rpc.Method{ID: "example/service::Greeter.Hello", Service: "example/service::Greeter", Name: "Hello", ContractHash: contractHash}
}

func TestRouterRevisionWatchAndPublicationReplace(t *testing.T) {
	method := testMethod()
	bundle, err := NewMRPCBundle("example/service", []MRPCFile{{Path: "service.mrpc", Text: "syntax = \"mrpc/v2\"; package service; service Greeter { Hello() (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	router := New(Options{})
	initial := router.Revision()
	if initial.RouterID == "" || initial.RouteEpoch != 0 {
		t.Fatalf("initial revision = %#v", initial)
	}
	watched := make(chan Revision, 1)
	go func() {
		revision, _ := router.WatchRevision(context.Background(), initial.RouterID, initial.RouteEpoch)
		watched <- revision
	}()
	first, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "first"), Bundles: []MRPCBundle{bundle}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case revision := <-watched:
		if revision.RouteEpoch <= initial.RouteEpoch || len(revision.References) != 1 {
			t.Fatalf("watched revision = %#v", revision)
		}
	case <-time.After(time.Second):
		t.Fatal("Router revision watch did not observe publication")
	}
	oldRoutes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	defer oldRoutes.Close()
	second, err := router.Replace(first, []ProviderEntry{{Provider: testProvider(t, method, "second"), Bundles: []MRPCBundle{bundle}}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.ForceClose(context.Background())
	result, err := oldRoutes.Call(context.Background(), rpc.Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if result.Values[0].Data != "first" {
		t.Fatalf("old route changed during replacement: %#v", result.Values)
	}
	_ = result.Discard(context.Background())
	if got := callValue(t, router, method); got != "second" {
		t.Fatalf("new route result = %q", got)
	}
}

func TestRouterPublicationPlanChangesVisibilityOnlyOnCommit(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	initial := router.Revision()
	plan, err := router.PreparePublish([]ProviderEntry{{Provider: testProvider(t, method, "prepared")}})
	if err != nil {
		t.Fatal(err)
	}
	if revision := router.Revision(); revision.RouteEpoch != initial.RouteEpoch {
		t.Fatalf("prepare changed route epoch from %d to %d", initial.RouteEpoch, revision.RouteEpoch)
	}
	if _, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}); err == nil {
		t.Fatal("prepared provider was visible before commit")
	}
	publication, err := plan.Commit()
	if err != nil {
		t.Fatal(err)
	}
	defer publication.ForceClose(context.Background())
	if got := callValue(t, router, method); got != "prepared" {
		t.Fatalf("committed value = %q", got)
	}
	if _, err := plan.Commit(); err == nil {
		t.Fatal("publication plan committed twice")
	}
}

func TestRouterPublicationPlanCanBeAbandoned(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	plan, err := router.PreparePublish([]ProviderEntry{{Provider: testProvider(t, method, "abandoned")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Commit(); err == nil {
		t.Fatal("abandoned publication plan committed")
	}
	if len(router.Status().Registrations) != 0 {
		t.Fatal("abandoned publication changed Router state")
	}
}

func testProvider(t *testing.T, method rpc.Method, value string) rpc.Provider {
	t.Helper()
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
		return []rpc.Value{{Type: "string", Data: value}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

type failingCloseProvider struct {
	rpc.Provider
	err error
}

func (p failingCloseProvider) BindRPC(ctx context.Context, request rpc.BindRequest) (rpc.ProviderLease, error) {
	lease, err := p.Provider.BindRPC(ctx, request)
	if err != nil {
		return nil, err
	}
	return failingCloseLease{ProviderLease: lease, err: p.err}, nil
}

type failingCloseLease struct {
	rpc.ProviderLease
	err error
}

func (l failingCloseLease) Close() error {
	return errors.Join(l.ProviderLease.Close(), l.err)
}

func callValue(t *testing.T, binder rpc.Binder, method rpc.Method) string {
	t.Helper()
	routes, err := binder.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Close()
	result, err := routes.Call(context.Background(), rpc.Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Discard(context.Background())
	value, ok := result.Values[0].Data.(string)
	if !ok {
		t.Fatalf("RPC result = %#v", result.Values)
	}
	return value
}

func TestRouterUsesNewProviderOnlyForNewBindings(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	first, err := router.Register(testProvider(t, method, "first"), RegistrationOptions{Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Close()
	if _, err := router.Register(testProvider(t, method, "second"), RegistrationOptions{Priority: 2}); err != nil {
		t.Fatal(err)
	}
	result, err := routes.Call(context.Background(), rpc.Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if result.Values[0].Data != "first" {
		t.Fatalf("existing route changed provider: %#v", result.Values)
	}
	_ = result.Discard(context.Background())
	if got := callValue(t, router, method); got != "second" {
		t.Fatalf("new route result = %q", got)
	}
	if status := first.Status(); status.ActiveLeases != 1 {
		t.Fatalf("first registration status = %#v", status)
	}
}

func TestRouterPublicationOwnsProviderAndManifestLifecycle(t *testing.T) {
	method := testMethod()
	bundle, err := NewMRPCBundle("example/service", []MRPCFile{{Path: "service.mrpc", Text: "syntax = \"mrpc/v2\"; package service; service Greeter { Hello() (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	invalid := bundle
	invalid.Files = append([]MRPCFile(nil), bundle.Files...)
	invalid.Files[0].Text += " broken"
	router := New(Options{})
	if _, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "value"), Bundles: []MRPCBundle{invalid}}}); err == nil {
		t.Fatal("invalid publication succeeded")
	}
	if len(router.Status().Registrations) != 0 || len(router.ContractReferences()) != 0 {
		t.Fatal("failed publication changed Router state")
	}
	publication, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "value"), Bundles: []MRPCBundle{bundle}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(router.ContractReferences()) != 1 || callValue(t, router, method) != "value" {
		t.Fatal("committed publication is incomplete")
	}
	if err := publication.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(router.ContractReferences()) != 0 {
		t.Fatal("closed publication remained in current manifest")
	}
	if _, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}); err == nil {
		t.Fatal("closed publication remained routable")
	}
	if _, err := router.ResolveContract(context.Background(), ContractReference{ImportPath: bundle.ImportPath, Hash: bundle.Hash}); err == nil {
		t.Fatal("closed publication retained its contract bundle")
	} else if code, _ := rpc.CodeOf(err); code != rpc.CodeNotFound {
		t.Fatalf("closed publication contract = %v", err)
	}
}

func TestRouterPublicationRejectsConflictingActiveContract(t *testing.T) {
	method := testMethod()
	first, err := NewMRPCBundle("example/service", []MRPCFile{{Path: "service.mrpc", Text: "syntax = \"mrpc/v2\"; package service; service Greeter { Hello() (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMRPCBundle("example/service", []MRPCFile{{Path: "service.mrpc", Text: "syntax = \"mrpc/v2\"; package service; service Greeter { Hello(name string) (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	router := New(Options{})
	publication, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "first"), Bundles: []MRPCBundle{first}}})
	if err != nil {
		t.Fatal(err)
	}
	defer publication.ForceClose(context.Background())
	if _, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "second"), Bundles: []MRPCBundle{second}}}); err == nil {
		t.Fatal("conflicting active contract publication succeeded")
	}
	if references := router.ContractReferences(); len(references) != 1 || references[0].Hash != first.Hash {
		t.Fatalf("failed publication changed manifest: %#v", references)
	}
}

func TestRouterPublicationReplacementFailureKeepsCurrentState(t *testing.T) {
	method := testMethod()
	bundle, err := NewMRPCBundle("example/service", []MRPCFile{{Path: "service.mrpc", Text: "syntax = \"mrpc/v2\"; package service; service Greeter { Hello() (value string, err error); }"}})
	if err != nil {
		t.Fatal(err)
	}
	invalid := bundle
	invalid.Files = append([]MRPCFile(nil), bundle.Files...)
	invalid.Files[0].Text += " broken"
	router := New(Options{})
	current, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "current"), Bundles: []MRPCBundle{bundle}}})
	if err != nil {
		t.Fatal(err)
	}
	defer current.ForceClose(context.Background())
	revision := router.Revision()
	if _, err := router.Replace(current, []ProviderEntry{{Provider: testProvider(t, method, "replacement"), Bundles: []MRPCBundle{invalid}}}); err == nil {
		t.Fatal("invalid replacement succeeded")
	}
	if got := callValue(t, router, method); got != "current" {
		t.Fatalf("failed replacement changed provider to %q", got)
	}
	after := router.Revision()
	if after.RouteEpoch != revision.RouteEpoch || len(after.References) != 1 || after.References[0].Hash != bundle.Hash {
		t.Fatalf("failed replacement changed revision: before=%#v after=%#v", revision, after)
	}
}

func TestRouterForceCloseReleasesOwnedProviderOnce(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	var closes atomic.Int32
	registration, err := router.RegisterOwned(testProvider(t, method, "value"), RegistrationOptions{}, func() error {
		closes.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registration.ForceClose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatalf("owned close count = %d", closes.Load())
	}
	if _, err := routes.Call(context.Background(), rpc.Call{Method: method}); err == nil {
		t.Fatal("force-closed route accepted a call")
	}
	_ = routes.Close()
}

func TestRouterForceShutdownClosesRetiredPublicationBindings(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	first, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "old")}})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Close()
	if _, err := router.Replace(first, []ProviderEntry{{Provider: testProvider(t, method, "new")}}); err != nil {
		t.Fatal(err)
	}
	if err := router.ForceShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := routes.Call(context.Background(), rpc.Call{Method: method}); err == nil {
		t.Fatal("ForceShutdown left a retired publication binding usable")
	}
}

func TestRouterShutdownWaitTimeoutRetainsRetiredRegistration(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	first, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "old")}})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Replace(first, []ProviderEntry{{Provider: testProvider(t, method, "new")}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := router.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown with a canceled wait = %v", err)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	if err := router.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after retired binding closed = %v", err)
	}
}

func TestRouterShutdownReportsRetiredCleanupFailure(t *testing.T) {
	method := testMethod()
	closeErr := errors.New("retired provider close failed")
	base := testProvider(t, method, "old")
	router := New(Options{})
	first, err := router.Publish([]ProviderEntry{{Provider: failingCloseProvider{Provider: base, err: closeErr}}})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Replace(first, []ProviderEntry{{Provider: testProvider(t, method, "new")}}); err != nil {
		t.Fatal(err)
	}
	if err := routes.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("retired route close = %v", err)
	}
	if err := router.Shutdown(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("Shutdown lost retired cleanup error: %v", err)
	}
}

func TestRouterSelectionMatchesLabelPresenceAndValue(t *testing.T) {
	for _, test := range []struct {
		name                string
		provider, requested map[string]string
		match               bool
	}{
		{name: "no filter", match: true},
		{name: "missing empty label", requested: map[string]string{"zone": ""}},
		{name: "explicit empty label", provider: map[string]string{"zone": ""}, requested: map[string]string{"zone": ""}, match: true},
		{name: "different value", provider: map[string]string{"zone": "local"}, requested: map[string]string{"zone": ""}},
		{name: "matching value", provider: map[string]string{"zone": "local", "env": "test"}, requested: map[string]string{"zone": "local"}, match: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			method := testMethod()
			router := New(Options{})
			registration, err := router.Register(testProvider(t, method, "selected"), RegistrationOptions{Labels: test.provider})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := registration.ForceClose(context.Background()); err != nil {
					t.Error(err)
				}
			})
			route, err := router.Bind(context.Background(), rpc.BindRequest{
				Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}},
				Options:  rpc.BindOptions{Labels: test.requested},
			})
			if route != nil {
				defer route.Close()
			}
			if !test.match {
				code, _ := rpc.CodeOf(err)
				if code != rpc.CodeUnavailable || route != nil {
					t.Fatalf("label mismatch bound route: %v, %v", route, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := route.Call(context.Background(), rpc.Call{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Discard(context.Background())
			if result.Values[0].Data != "selected" {
				t.Fatalf("provider result = %v", result.Values)
			}
		})
	}
}

func TestRouterSelectionHonorsPriorityCapacityLabelsAndHealth(t *testing.T) {
	method := testMethod()
	router := New(Options{})
	low, err := router.Register(testProvider(t, method, "low"), RegistrationOptions{Priority: 1, Labels: map[string]string{"zone": "local"}})
	if err != nil {
		t.Fatal(err)
	}
	high, err := router.Register(testProvider(t, method, "high"), RegistrationOptions{Priority: 2, MaxLeases: 1, Labels: map[string]string{"zone": "local"}})
	if err != nil {
		t.Fatal(err)
	}
	bind := func(labels map[string]string) (*rpc.RouteSet, error) {
		return router.Bind(context.Background(), rpc.BindRequest{
			Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}},
			Options:  rpc.BindOptions{Labels: labels},
		})
	}
	first, err := bind(map[string]string{"zone": "local"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := bind(map[string]string{"zone": "local"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := second.Call(context.Background(), rpc.Call{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	if result.Values[0].Data != "low" {
		t.Fatalf("capacity fallback = %#v", result.Values)
	}
	_ = result.Discard(context.Background())
	_ = second.Close()
	high.SetHealthy(false)
	if got := callValue(t, router, method); got != "low" {
		t.Fatalf("health fallback = %q", got)
	}
	if _, err := bind(map[string]string{"zone": "remote"}); err == nil {
		t.Fatal("label mismatch was routed")
	}
	if err := low.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type mountedTestResource struct{ closes atomic.Int32 }

func (r *mountedTestResource) Invoke(_ context.Context, method string, _ []rpc.Value) ([]rpc.Value, error) {
	if method != "Read" {
		return nil, rpc.StatusError{Code: rpc.CodeNotFound, Message: "unknown resource method"}
	}
	return []rpc.Value{{Type: "string", Data: "contents"}}, nil
}

func (r *mountedTestResource) Close(context.Context) error {
	r.closes.Add(1)
	return nil
}

func TestRouterMountRehomesRemoteResourcesAndBoundsLoops(t *testing.T) {
	open := rpc.Method{ID: "example/files::Files.Open", Service: "example/files::Files", Name: "Open", ContractHash: contractHash}
	read := rpc.Method{ID: "example/files::File.Read", Service: "example/files::File", Name: "Read", ContractHash: contractHash, ResourceTypeHash: contractHash}
	resource := &mountedTestResource{}
	provider, err := rpc.NewProvider(
		rpc.MethodBinding{Method: open, Invoke: func(ctx context.Context, _ []rpc.Value) ([]rpc.Value, error) {
			value, err := rpc.Export(ctx, resource, contractHash)
			return []rpc.Value{value}, err
		}},
		rpc.MethodBinding{Method: read},
	)
	if err != nil {
		t.Fatal(err)
	}
	contract := rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{open, read}}
	remote := New(Options{})
	if _, err := remote.Register(provider, RegistrationOptions{}); err != nil {
		t.Fatal(err)
	}
	local := New(Options{})
	if _, err := local.Mount(remote, contract, RegistrationOptions{}); err != nil {
		t.Fatal(err)
	}
	routes, err := local.Bind(context.Background(), rpc.BindRequest{Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := routes.Call(context.Background(), rpc.Call{Method: open})
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	ref := opened.Values[0].Resource
	if ref == nil || ref.Epoch != routes.Epoch() {
		t.Fatalf("mounted resource = %#v", ref)
	}
	result, err := routes.Call(context.Background(), rpc.Call{Method: read, Receiver: ref})
	if err != nil || result.Values[0].Data != "contents" {
		t.Fatalf("mounted resource call = %#v, err=%v", result, err)
	}
	_ = result.Discard(context.Background())
	if err := routes.Drop(context.Background(), *ref); err != nil {
		t.Fatal(err)
	}
	if resource.closes.Load() != 1 {
		t.Fatalf("remote resource close count = %d", resource.closes.Load())
	}
	_ = routes.Close()

	loop := New(Options{})
	if _, err := loop.Mount(loop, rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{open}}, RegistrationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{open}}}); err == nil {
		t.Fatal("Router mount loop exceeded its hop budget")
	}
}
