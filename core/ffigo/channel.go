package ffigo

import (
	"context"
	"sync"
)

type ChannelDirection uint8

const (
	ChannelBoth ChannelDirection = iota
	ChannelRecv
	ChannelSend
)

func (d ChannelDirection) CanRecv() bool {
	return d == ChannelBoth || d == ChannelRecv
}

func (d ChannelDirection) CanSend() bool {
	return d == ChannelBoth || d == ChannelSend
}

type ChannelEndpoint interface {
	ElemType() string
	Direction() ChannelDirection
	Recv(context.Context) ([]byte, bool, error)
	Send(context.Context, []byte) error
	Close() error
}

type ChannelTryReceiver interface {
	TryRecv() ([]byte, bool, bool, error)
}

type ChannelTrySender interface {
	TrySend([]byte) (bool, error)
}

type ChannelEndpointFuncs struct {
	Elem      string
	Dir       ChannelDirection
	OnRecv    func(context.Context) ([]byte, bool, error)
	OnTryRecv func() ([]byte, bool, bool, error)
	OnSend    func(context.Context, []byte) error
	OnTrySend func([]byte) (bool, error)
	OnClose   func() error
}

func (c ChannelEndpointFuncs) ElemType() string { return c.Elem }
func (c ChannelEndpointFuncs) Direction() ChannelDirection {
	return c.Dir
}

func (c ChannelEndpointFuncs) Recv(ctx context.Context) ([]byte, bool, error) {
	if c.OnRecv == nil {
		return nil, false, nil
	}
	return c.OnRecv(ctx)
}

func (c ChannelEndpointFuncs) TryRecv() ([]byte, bool, bool, error) {
	if c.OnTryRecv == nil {
		return nil, false, false, nil
	}
	return c.OnTryRecv()
}

func (c ChannelEndpointFuncs) Send(ctx context.Context, data []byte) error {
	if c.OnSend == nil {
		return nil
	}
	return c.OnSend(ctx, data)
}

func (c ChannelEndpointFuncs) TrySend(data []byte) (bool, error) {
	if c.OnTrySend == nil {
		return false, nil
	}
	return c.OnTrySend(data)
}

func (c ChannelEndpointFuncs) Close() error {
	if c.OnClose == nil {
		return nil
	}
	return c.OnClose()
}

type ChannelRegistry interface {
	RegisterChannel(ChannelEndpoint) uint64
	LookupChannel(uint64) (ChannelEndpoint, bool)
	UnregisterChannel(uint64) bool
}

type channelRegistry struct {
	mu     sync.RWMutex
	nextID uint64
	items  map[uint64]ChannelEndpoint
}

func NewChannelRegistry() ChannelRegistry {
	return &channelRegistry{items: make(map[uint64]ChannelEndpoint)}
}

func (r *channelRegistry) RegisterChannel(endpoint ChannelEndpoint) uint64 {
	if r == nil || endpoint == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.items[id] = endpoint
	return id
}

func (r *channelRegistry) LookupChannel(id uint64) (ChannelEndpoint, bool) {
	if r == nil || id == 0 {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	endpoint, ok := r.items[id]
	return endpoint, ok
}

func (r *channelRegistry) UnregisterChannel(id uint64) bool {
	if r == nil || id == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return false
	}
	delete(r.items, id)
	return true
}

type channelRegistryContextKey struct{}

func ContextWithChannelRegistry(ctx context.Context, registry ChannelRegistry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if registry == nil {
		return ctx
	}
	return context.WithValue(ctx, channelRegistryContextKey{}, registry)
}

func ChannelRegistryFromContext(ctx context.Context) ChannelRegistry {
	if ctx == nil {
		return nil
	}
	registry, _ := ctx.Value(channelRegistryContextKey{}).(ChannelRegistry)
	return registry
}
