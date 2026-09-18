package rpc

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

type localBinderProvider struct {
	contract Contract
	bind     func(context.Context, BindRequest) (ProviderLease, error)
}

func (p localBinderProvider) RPCContract() Contract { return p.contract }
func (p localBinderProvider) BindRPC(ctx context.Context, request BindRequest) (ProviderLease, error) {
	return p.bind(ctx, request)
}

type localBinderLease struct{ closes *atomic.Int32 }

func (l localBinderLease) Invoke(context.Context, Method, []Value) (*ProviderResult, error) {
	return &ProviderResult{}, nil
}

func (l localBinderLease) Close() error {
	if l.closes.Add(1) != 1 {
		return errors.New("provider lease closed more than once")
	}
	return nil
}

func TestLocalBinderKeepsPerServiceLeasesForSharedProvider(t *testing.T) {
	first := Method{ID: "a::Service.Call", Service: "a::Service", Name: "Call", ContractHash: testContractHash}
	second := Method{ID: "b::Service.Call", Service: "b::Service", Name: "Call", ContractHash: testContractHash}
	resource := Method{ID: "b::File.Read", Service: "b::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
	var bound []string
	var closes []*atomic.Int32
	provider := localBinderProvider{contract: testContract(first, second, resource), bind: func(_ context.Context, request BindRequest) (ProviderLease, error) {
		if len(request.Contract.Methods) != 1 {
			t.Fatalf("service binding: %+v", request.Contract.Methods)
		}
		bound = append(bound, request.Contract.Methods[0].Service)
		count := new(atomic.Int32)
		closes = append(closes, count)
		return localBinderLease{closes: count}, nil
	}}
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	provider.contract.Methods[0].ContractHash = testResourceHash
	routes, err := binder.Bind(context.Background(), testBindRequest(second, resource, first))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = routes.Close() })
	if !reflect.DeepEqual(bound, []string{first.Service, second.Service}) {
		t.Fatalf("bound services: %v", bound)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	for _, count := range closes {
		if count.Load() != 1 {
			t.Fatalf("lease close count = %d", count.Load())
		}
	}
}

func TestLocalBinderRollsBackPartialBinding(t *testing.T) {
	first := Method{ID: "a::Service.Call", Service: "a::Service", Name: "Call", ContractHash: testContractHash}
	second := Method{ID: "b::Service.Call", Service: "b::Service", Name: "Call", ContractHash: testContractHash}
	for _, test := range []struct {
		name         string
		code         Code
		secondCloses int32
	}{
		{"missing service", CodeUnimplemented, 0},
		{"contract mismatch", CodeFailedPrecondition, 0},
		{"bind error", CodeUnavailable, 0},
		{"lease with error", CodeUnavailable, 1},
		{"nil lease", CodeInternal, 0},
		{"canceled binding", CodeCanceled, 1},
		{"missing resource", CodeUnimplemented, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var firstCloses, secondCloses atomic.Int32
			providers := []Provider{
				localBinderProvider{contract: testContract(first), bind: func(context.Context, BindRequest) (ProviderLease, error) {
					return localBinderLease{closes: &firstCloses}, nil
				}},
			}
			if test.name != "missing service" {
				providers = append(providers, localBinderProvider{
					contract: testContract(second),
					bind: func(context.Context, BindRequest) (ProviderLease, error) {
						switch test.name {
						case "contract mismatch":
							t.Fatal("bound a provider with a mismatched contract")
						case "bind error":
							return nil, StatusError{Code: CodeUnavailable, Message: "offline"}
						case "lease with error":
							return localBinderLease{closes: &secondCloses}, StatusError{Code: CodeUnavailable, Message: "offline"}
						case "nil lease":
							return nil, nil
						case "canceled binding":
							cancel()
						}
						return localBinderLease{closes: &secondCloses}, nil
					},
				})
			}
			request := testBindRequest(first, second)
			switch test.name {
			case "contract mismatch":
				request.Contract.Methods[1].ContractHash = testResourceHash
			case "missing resource":
				request.Contract.Methods = append(request.Contract.Methods, Method{
					ID: "b::Resource.Read", Service: "b::Resource", Name: "Read",
					ContractHash: testContractHash, ResourceTypeHash: testResourceHash,
				})
			}
			binder, err := NewLocalBinder(LocalBinderOptions{}, providers...)
			if err != nil {
				t.Fatal(err)
			}
			routes, err := binder.Bind(ctx, request)
			if routes != nil {
				_ = routes.Close()
				t.Fatal("partial binding returned routes")
			}
			if codeOf(err) != test.code {
				t.Fatalf("bind error = %v, want %v", err, test.code)
			}
			if firstCloses.Load() != 1 || secondCloses.Load() != test.secondCloses {
				t.Fatalf("lease closes = (%d, %d), want (1, %d)", firstCloses.Load(), secondCloses.Load(), test.secondCloses)
			}
		})
	}
}

func TestLocalBinderTransfersLeaseOwnership(t *testing.T) {
	method := Method{ID: "a::Service.Call", Service: "a::Service", Name: "Call", ContractHash: testContractHash}
	var closes atomic.Int32
	provider := localBinderProvider{contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) {
		return localBinderLease{closes: &closes}, nil
	}}
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := binder.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = routes.Close() })
	if closes.Load() != 0 {
		t.Fatal("binding closed its transferred lease")
	}
	for i := 0; i < 2; i++ {
		if err := routes.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if closes.Load() != 1 {
		t.Fatalf("route close count = %d, want 1", closes.Load())
	}
}

func TestRouteSetFailureRetainsCallerOwnership(t *testing.T) {
	first := Method{ID: "a::Service.Call", Service: "a::Service", Name: "Call", ContractHash: testContractHash}
	second := Method{ID: "b::Service.Call", Service: "b::Service", Name: "Call", ContractHash: testContractHash}
	var closes atomic.Int32
	lease := localBinderLease{closes: &closes}
	routes, err := NewRouteSet(Limits{}, testContract(first, second), map[string]ProviderLease{first.Service: lease})
	if routes != nil {
		_ = routes.Close()
		t.Fatal("incomplete route set succeeded")
	}
	if codeOf(err) != CodeUnavailable {
		t.Fatalf("route error = %v, want unavailable", err)
	}
	if closes.Load() != 0 {
		t.Fatal("failed route set closed a caller-owned lease")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBinderRejectsDuplicateService(t *testing.T) {
	method := Method{ID: "a::Service.Call", Service: "a::Service", Name: "Call", ContractHash: testContractHash}
	provider := func() Provider {
		return localBinderProvider{contract: testContract(method), bind: func(context.Context, BindRequest) (ProviderLease, error) {
			return localBinderLease{closes: new(atomic.Int32)}, nil
		}}
	}
	if _, err := NewLocalBinder(LocalBinderOptions{}, provider(), provider()); err == nil {
		t.Fatal("duplicate service providers were accepted")
	}
}
