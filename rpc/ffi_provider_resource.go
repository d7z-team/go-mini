package rpc

import (
	"context"
	"sync"
)

type guestProviderResource struct {
	mu       sync.Mutex
	provider *ffiProvider
	ref      ResourceRef
	state    providerResourceState
	done     chan struct{}
}

type providerResourceState uint8

const (
	providerResourceOpen providerResourceState = iota
	providerResourceReleasing
	providerResourceClosed
)

func (r *guestProviderResource) Invoke(ctx context.Context, method string, arguments []Value) ([]Value, error) {
	if r == nil {
		return nil, StatusError{Code: CodeNotFound, Message: "MRPC guest resource is closed"}
	}
	r.mu.Lock()
	provider, ref, open := r.provider, r.ref, r.state == providerResourceOpen
	r.mu.Unlock()
	if provider == nil || !open {
		return nil, StatusError{Code: CodeNotFound, Message: "MRPC guest resource is closed"}
	}
	contractMethod, ok := provider.resourceMethod(ref.TypeHash, method)
	if !ok {
		return nil, StatusError{Code: CodeNotFound, Message: "MRPC guest resource method is unavailable"}
	}
	result, err := provider.invoke(ctx, contractMethod, &ref, arguments)
	if err != nil {
		return nil, err
	}
	return result.Values, nil
}

func (r *guestProviderResource) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.state == providerResourceClosed || r.provider == nil {
		r.mu.Unlock()
		return nil
	}
	if r.state == providerResourceReleasing {
		done := r.done
		r.mu.Unlock()
		select {
		case <-done:
			return r.Close(ctx)
		case <-ctx.Done():
			return statusError(ctx.Err())
		}
	}
	r.state = providerResourceReleasing
	r.done = make(chan struct{})
	done, provider, ref := r.done, r.provider, r.ref
	r.mu.Unlock()

	var err error
	select {
	case provider.events <- ffiProviderEvent{kind: "release", receiver: &ref}:
	case <-provider.done:
	case <-ctx.Done():
		err = statusError(ctx.Err())
	}

	r.mu.Lock()
	if err == nil {
		r.state = providerResourceClosed
		r.provider = nil
	} else {
		r.state = providerResourceOpen
	}
	close(done)
	r.done = nil
	r.mu.Unlock()
	return err
}

func (p *ffiProvider) resourceMethod(typeHash, name string) (Method, bool) {
	for _, method := range p.contract.Methods {
		if method.ResourceTypeHash == typeHash && method.Name == name {
			return method, true
		}
	}
	return Method{}, false
}
