package rpc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

var routeEpoch atomic.Uint64

// LocalBinderOptions configures an immutable in-process provider set.
type LocalBinderOptions struct {
	Limits Limits
}

// LocalBinder binds each service to the unique Provider fixed at construction.
// Each successful Bind transfers ProviderLease ownership to its RouteSet.
// The binder never owns Providers.
type LocalBinder struct {
	limits   Limits
	services map[string]localService
}

type localService struct {
	provider Provider
	methods  []Method
}

// NewLocalBinder validates and fixes a unique provider for every service.
func NewLocalBinder(options LocalBinderOptions, providers ...Provider) (*LocalBinder, error) {
	limits := normalizeLimits(options.Limits)
	binder := &LocalBinder{
		limits: limits, services: make(map[string]localService),
	}
	for _, provider := range providers {
		if provider == nil {
			return nil, errors.New("local RPC binder contains a nil provider")
		}
		contract, err := normalizeContract(provider.RPCContract(), limits.MaxMethods)
		if err != nil {
			return nil, err
		}
		services := make(map[string]struct{})
		for _, method := range contract.Methods {
			if method.ResourceTypeHash == "" {
				services[method.Service] = struct{}{}
			}
		}
		if len(services) == 0 {
			return nil, errors.New("local RPC provider has no service methods")
		}
		for service := range services {
			if _, duplicate := binder.services[service]; duplicate {
				return nil, fmt.Errorf("local RPC service %q has multiple providers", service)
			}
			binder.services[service] = localService{provider: provider, methods: contract.Methods}
		}
	}
	return binder, nil
}

// Bind fixes the requested contract to one ProviderLease per service.
func (b *LocalBinder) Bind(ctx context.Context, request BindRequest) (*RouteSet, error) {
	if b == nil {
		return nil, errors.New("local RPC binder is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, statusError(err)
	}
	contract, err := normalizeContract(request.Contract, b.limits.MaxMethods)
	if err != nil {
		return nil, err
	}
	groups := make(map[string][]Method)
	var resources []Method
	for _, method := range contract.Methods {
		if method.ResourceTypeHash == "" {
			groups[method.Service] = append(groups[method.Service], method)
		} else {
			resources = append(resources, method)
		}
	}
	services := make([]string, 0, len(groups))
	for service := range groups {
		services = append(services, service)
	}
	sort.Strings(services)
	leases := make(map[string]ProviderLease, len(services))
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, service := range services {
			if lease := leases[service]; lease != nil {
				_ = lease.Close()
			}
		}
	}()
	for _, service := range services {
		implementation, ok := b.services[service]
		if !ok {
			return nil, StatusError{Code: CodeUnimplemented, Message: "RPC service is not implemented: " + service}
		}
		if err := CheckMethodSupport(implementation.methods, groups[service]); err != nil {
			return nil, err
		}
		lease, bindErr := implementation.provider.BindRPC(ctx, BindRequest{
			Contract: Contract{Protocol: ContractProtocol, Methods: groups[service]},
			Options:  request.Options, Peer: request.Peer, Provider: request.Provider, Hops: request.Hops,
		})
		if bindErr != nil {
			if lease != nil {
				_ = lease.Close()
			}
			return nil, statusError(bindErr)
		}
		if lease == nil {
			return nil, StatusError{Code: CodeInternal, Message: "RPC provider returned a nil lease"}
		}
		if err := ctx.Err(); err != nil {
			_ = lease.Close()
			return nil, statusError(err)
		}
		leases[service] = lease
	}
	if len(resources) != 0 {
		var available []Method
		for _, service := range services {
			available = append(available, b.services[service].methods...)
		}
		if err := CheckMethodSupport(available, resources); err != nil {
			return nil, err
		}
	}
	routes, err := NewRouteSet(b.limits, contract, leases)
	if err != nil {
		return nil, err
	}
	committed = true
	return routes, nil
}

// NewRouteSet transfers ProviderLease ownership into a route transaction on success.
// On failure the caller retains ownership of every lease.
// Binder implementations use it after selecting and binding every service.
func NewRouteSet(limits Limits, contract Contract, leases map[string]ProviderLease) (*RouteSet, error) {
	limits = normalizeLimits(limits)
	normalized, err := normalizeContract(contract, limits.MaxMethods)
	if err != nil {
		return nil, err
	}
	for service, provider := range leases {
		if service == "" || provider == nil {
			return nil, errors.New("RPC route set contains an invalid service lease")
		}
	}
	for _, method := range normalized.Methods {
		if method.ResourceTypeHash == "" && leases[method.Service] == nil {
			return nil, StatusError{Code: CodeUnavailable, Message: "RPC route set is missing service: " + method.Service}
		}
	}
	epoch := routeEpoch.Add(1)
	if epoch == 0 {
		return nil, StatusError{Code: CodeResourceExhausted, Message: "RPC route epoch space exhausted"}
	}
	services := make(map[string]*serviceLease, len(leases))
	for service, provider := range leases {
		services[service] = newServiceLease(provider)
	}
	return newRouteSet(epoch, limits, normalized.Methods, services), nil
}

type serviceLease struct {
	once     sync.Once
	mu       sync.Mutex
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	calls    sync.WaitGroup
	provider ProviderLease
	err      error
}

func newServiceLease(provider ProviderLease) *serviceLease {
	ctx, cancel := context.WithCancel(context.Background())
	return &serviceLease{ctx: ctx, cancel: cancel, provider: provider}
}

func (l *serviceLease) Invoke(ctx context.Context, method Method, arguments []Value) (*ProviderResult, error) {
	if l == nil || l.provider == nil {
		return nil, StatusError{Code: CodeUnavailable, Message: "RPC provider lease is closed"}
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, StatusError{Code: CodeUnavailable, Message: "RPC provider lease is closed"}
	}
	l.calls.Add(1)
	leaseCtx := l.ctx
	l.mu.Unlock()
	defer l.calls.Done()
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(leaseCtx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	return l.provider.Invoke(callCtx, method, arguments)
}

func (l *serviceLease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.cancel()
		l.mu.Unlock()
		l.calls.Wait()
		l.err = l.provider.Close()
	})
	return l.err
}
