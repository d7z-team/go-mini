package rpc

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestHostSessionCapacityIncludesRetiringCleanup(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	lease := &blockingBindingLease{started: make(chan struct{}), release: release}
	method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	host, err := NewHost(HostOptions{MaxSessions: 1, Providers: []Provider{localBinderProvider{
		contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) { return lease, nil },
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unblock(); _ = host.Close() })
	first, err := host.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.(*ffiSession).open(t.Context(), testContract(method), BindOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := first.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-lease.started
	if _, err := host.Open(t.Context()); codeOf(err) != CodeResourceExhausted {
		t.Fatalf("retiring session capacity: %v", err)
	}
	unblock()
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	second, err := host.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
