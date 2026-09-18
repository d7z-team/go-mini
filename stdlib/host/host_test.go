package host

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
)

func TestHostShutdownWaitsForBackendCleanup(t *testing.T) {
	provider, err := rpc.NewProvider(rpc.MethodBinding{
		Method: rpc.Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	wantErr := errors.New("backend cleanup failed")
	host, err := New(Options{Providers: []Provider{{Capability: "test", RPC: provider, Close: func() error { close(started); <-release; return wantErr }}}})
	if err != nil {
		t.Fatal(err)
	}
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(func() {
		unblock()
		_ = host.Close()
	})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := host.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-started
	if _, err := host.Open(context.Background()); err == nil {
		t.Fatal("opened host during cleanup")
	}
	if err := host.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("second wait = %v", err)
	}
	unblock()
	for range 2 {
		if err := host.Close(); !errors.Is(err, wantErr) {
			t.Fatalf("close = %v", err)
		}
	}
}

func TestHostOwnsCapabilityCleanupInReverseOrder(t *testing.T) {
	providerFor := func(service string) rpc.Provider {
		method := rpc.Method{
			ID: service + ".Call", Service: service, Name: "Call",
			ContractHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}
		provider, err := rpc.NewProvider(rpc.MethodBinding{
			Method: method,
			Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) { return nil, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return provider
	}
	var closed []string
	host, err := New(Options{
		Required: []string{"first"},
		Providers: []Provider{
			{Capability: "first", RPC: providerFor("test.host.v1::First"), Close: func() error { closed = append(closed, "first"); return nil }},
			{Capability: "second", RPC: providerFor("test.host.v1::Second"), Close: func() error { closed = append(closed, "second"); return nil }},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := host.HostCapabilities(); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("installed capabilities = %v", got)
	}
	session, err := host.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"second", "first"}; !reflect.DeepEqual(closed, want) {
		t.Fatalf("cleanup order = %v, want %v", closed, want)
	}
}

func TestHostConstructionRollsBackTransferredProviders(t *testing.T) {
	method := rpc.Method{
		ID: "test.host.v1::Service.Call", Service: "test.host.v1::Service", Name: "Call",
		ContractHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	provider, err := rpc.NewProvider(rpc.MethodBinding{
		Method: method,
		Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		required      []string
		first, second string
		missingRPC    bool
		want          string
	}{
		{"empty_requirement", []string{""}, "first", "second", false, "non-empty capability"},
		{"duplicate_requirement", []string{"first", "first"}, "first", "second", false, "required more than once"},
		{"missing_provider", []string{"missing"}, "first", "second", false, "has no provider"},
		{"empty_capability", nil, "", "second", false, "provider is incomplete"},
		{"missing_rpc", nil, "first", "second", true, "provider is incomplete"},
		{"duplicate_capability", nil, "first", "first", false, "multiple providers"},
		{"duplicate_rpc", nil, "first", "second", false, "multiple providers"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var closed []string
			options := Options{
				Required: test.required,
				Providers: []Provider{
					{Capability: test.first, RPC: provider, Close: func() error { closed = append(closed, "first"); return nil }},
					{Capability: test.second, RPC: provider, Close: func() error { closed = append(closed, "second"); return nil }},
				},
			}
			if test.missingRPC {
				options.Providers[1].RPC = nil
			}
			host, err := New(options)
			if host != nil {
				_ = host.Close()
				t.Fatal("construction returned a host for invalid providers")
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New = %v, want %q", err, test.want)
			}
			if !reflect.DeepEqual(closed, []string{"second", "first"}) {
				t.Fatalf("rollback order = %v", closed)
			}
		})
	}
	closeErr := errors.New("close failed")
	host, err := New(Options{
		Required:  []string{"first"},
		Providers: []Provider{{Capability: "first", RPC: provider, Close: func() error { return closeErr }}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close error = %v, want %v", err, closeErr)
	}
}
