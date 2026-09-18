package rpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type staticProvider struct {
	contract Contract
	handlers map[Method]MethodHandler
	id       string
}

// IdentifiedProvider exposes the stable declaration identity of a provider.
// Gateways use it as the default registration name when the embedding layer
// does not supply deployment-specific routing options.
type IdentifiedProvider interface {
	Provider
	RPCProviderID() string
}

// NewProvider exposes immutable Go method bindings as an RPC provider.
func NewProvider(bindings ...MethodBinding) (Provider, error) {
	provider := &staticProvider{handlers: make(map[Method]MethodHandler, len(bindings))}
	for _, binding := range bindings {
		if err := validateMethod(binding.Method); err != nil {
			return nil, err
		}
		if binding.Invoke == nil && binding.Method.ResourceTypeHash == "" {
			return nil, fmt.Errorf("rpc method %q has no handler", binding.Method.ID)
		}
		if _, exists := provider.handlers[binding.Method]; exists {
			return nil, fmt.Errorf("duplicate rpc method %q", binding.Method.ID)
		}
		provider.contract.Methods = append(provider.contract.Methods, binding.Method)
		provider.handlers[binding.Method] = binding.Invoke
	}
	if len(provider.contract.Methods) == 0 {
		return nil, errors.New("rpc provider catalog is empty")
	}
	provider.contract.Protocol = ContractProtocol
	for _, method := range provider.contract.Methods {
		if method.ResourceTypeHash == "" && (provider.id == "" || method.Service < provider.id) {
			provider.id = method.Service
		}
	}
	return provider, nil
}

func (p *staticProvider) RPCContract() Contract {
	return cloneContract(p.contract)
}

func (p *staticProvider) RPCProviderID() string { return p.id }

func (p *staticProvider) BindRPC(ctx context.Context, request BindRequest) (ProviderLease, error) {
	contract, err := NormalizeContract(request.Contract, Limits{})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, statusError(err)
	}
	if err := CheckMethodSupport(p.contract.Methods, contract.Methods); err != nil {
		return nil, err
	}
	handlers := make(map[Method]MethodHandler, len(contract.Methods))
	for _, method := range contract.Methods {
		handler := p.handlers[method]
		if handler != nil {
			handlers[method] = handler
		}
	}
	providerID := request.Provider
	if providerID == "" {
		providerID = p.id
	}
	return &staticProviderLease{handlers: handlers, peer: clonePeerInfo(request.Peer), provider: providerID}, nil
}

type staticProviderLease struct {
	mu       sync.RWMutex
	handlers map[Method]MethodHandler
	peer     PeerInfo
	provider string
	closed   bool
}

func (l *staticProviderLease) Invoke(ctx context.Context, method Method, arguments []Value) (*ProviderResult, error) {
	l.mu.RLock()
	handler := l.handlers[method]
	closed := l.closed
	l.mu.RUnlock()
	if closed || handler == nil {
		return nil, StatusError{Code: CodeUnavailable, Message: "rpc provider binding is closed"}
	}
	ctx = withCallInfo(ctx, method, l.peer, l.provider)
	values, err := handler(ctx, append([]Value(nil), arguments...))
	if err != nil {
		return nil, err
	}
	return &ProviderResult{Values: values}, nil
}

func (l *staticProviderLease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.closed = true
	l.handlers = nil
	l.mu.Unlock()
	return nil
}
