package router

import (
	"context"
	"errors"

	"github.com/d7z-team/mini-go/rpc"
)

// Mount registers a remote Binder behind this Router.
func (g *Router) Mount(binder rpc.Binder, contract rpc.Contract, options RegistrationOptions) (*Registration, error) {
	if g == nil {
		return nil, errors.New("RPC Router is required")
	}
	provider, err := newMountedProvider(binder, contract, g.limits)
	if err != nil {
		return nil, err
	}
	return g.Register(provider, options)
}

func newMountedProvider(binder rpc.Binder, contract rpc.Contract, limits rpc.Limits) (*mountedProvider, error) {
	if binder == nil {
		return nil, errors.New("RPC mount binder is required")
	}
	normalized, err := rpc.NormalizeContract(contract, limits)
	if err != nil {
		return nil, err
	}
	provider := &mountedProvider{binder: binder, contract: normalized, resources: make(map[string]map[string]rpc.Method)}
	for _, method := range normalized.Methods {
		if method.ResourceTypeHash == "" {
			continue
		}
		if provider.resources[method.ResourceTypeHash] == nil {
			provider.resources[method.ResourceTypeHash] = make(map[string]rpc.Method)
		}
		provider.resources[method.ResourceTypeHash][method.Name] = method
	}
	return provider, nil
}

type mountedProvider struct {
	binder    rpc.Binder
	contract  rpc.Contract
	resources map[string]map[string]rpc.Method
}

func (p *mountedProvider) RPCContract() rpc.Contract {
	return rpc.Contract{Protocol: p.contract.Protocol, Methods: append([]rpc.Method(nil), p.contract.Methods...)}
}

func (p *mountedProvider) BindRPC(ctx context.Context, request rpc.BindRequest) (rpc.ProviderLease, error) {
	if request.Hops <= 0 {
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC Router hop limit exceeded"}
	}
	methods := append([]rpc.Method(nil), request.Contract.Methods...)
	for _, method := range p.contract.Methods {
		if method.ResourceTypeHash != "" {
			methods = append(methods, method)
		}
	}
	routes, err := p.binder.Bind(ctx, rpc.BindRequest{
		Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: methods},
		Options:  request.Options, Peer: clonePeer(request.Peer), Provider: request.Provider, Hops: request.Hops,
	})
	if err != nil {
		return nil, err
	}
	return &mountedProviderLease{routes: routes, resources: p.resources}, nil
}

type mountedProviderLease struct {
	routes    *rpc.RouteSet
	resources map[string]map[string]rpc.Method
}

func (l *mountedProviderLease) Invoke(ctx context.Context, method rpc.Method, arguments []rpc.Value) (*rpc.ProviderResult, error) {
	result, err := l.routes.Call(ctx, rpc.Call{Method: method, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	values, err := l.proxyValues(ctx, result.Values)
	if err != nil {
		_ = result.Discard(context.Background())
		return nil, err
	}
	return rpc.NewProviderResult(values, func(accept bool) error {
		if accept {
			return result.Accept(context.Background())
		}
		return result.Discard(context.Background())
	}), nil
}

func (l *mountedProviderLease) proxyValues(ctx context.Context, values []rpc.Value) ([]rpc.Value, error) {
	out := append([]rpc.Value(nil), values...)
	for index, value := range values {
		if value.Resource == nil {
			continue
		}
		methods := l.resources[value.Resource.TypeHash]
		if len(methods) == 0 {
			return nil, rpc.StatusError{Code: rpc.CodeProtocol, Message: "RPC proxy received an unknown resource type"}
		}
		proxied, err := rpc.Export(ctx, &mountedResource{routes: l.routes, ref: *value.Resource, methods: methods, resources: l.resources}, value.Resource.TypeHash)
		if err != nil {
			return nil, err
		}
		out[index] = proxied
	}
	return out, nil
}

func (l *mountedProviderLease) Close() error {
	if l == nil || l.routes == nil {
		return nil
	}
	return l.routes.Shutdown(context.Background())
}

type mountedResource struct {
	routes    *rpc.RouteSet
	ref       rpc.ResourceRef
	methods   map[string]rpc.Method
	resources map[string]map[string]rpc.Method
}

func (r *mountedResource) Invoke(ctx context.Context, name string, arguments []rpc.Value) ([]rpc.Value, error) {
	method, ok := r.methods[name]
	if !ok {
		return nil, rpc.StatusError{Code: rpc.CodeNotFound, Message: "RPC proxy resource method is unavailable"}
	}
	result, err := r.routes.Call(ctx, rpc.Call{Method: method, Receiver: &r.ref, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	lease := mountedProviderLease{routes: r.routes, resources: r.resources}
	values, err := lease.proxyValues(ctx, result.Values)
	if err != nil {
		_ = result.Discard(context.Background())
		return nil, err
	}
	if err := result.Accept(ctx); err != nil {
		return nil, err
	}
	return values, nil
}

func (r *mountedResource) Close(ctx context.Context) error {
	return r.routes.Drop(ctx, r.ref)
}
