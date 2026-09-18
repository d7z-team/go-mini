// Package host composes standard-library host capabilities for a Mini-Go instance.
package host

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/d7z-team/mini-go/ffi"
	"github.com/d7z-team/mini-go/rpc"
)

// Provider associates one runtime provider with the source capability it satisfies.
type Provider struct {
	Capability string
	RPC        rpc.Provider
	Close      func() error
}

// Options describes installed providers and the minimum required capabilities.
type Options struct {
	Required        []string
	Providers       []Provider
	Fallback        rpc.Binder
	PublishProvider rpc.PublishProviderFunc
	Limits          rpc.Limits
}

// Host owns the RPC bridge and explicitly owned module backends.
type Host struct {
	rpcHost      *rpc.Host
	cleanups     []func() error
	capabilities []string
	mu           sync.Mutex
	closed       bool
	once         sync.Once
	done         chan struct{}
	err          error
}

var _ ffi.CapabilityBridge = (*Host)(nil)

// New validates required capabilities and constructs a host transactionally.
// Ownership of every non-nil Provider.Close transfers to New, including when
// construction fails.
func New(options Options) (host *Host, err error) {
	cleanups := make([]func() error, 0, len(options.Providers))
	for _, provider := range options.Providers {
		if provider.Close != nil {
			cleanups = append(cleanups, provider.Close)
		}
	}
	defer func() {
		if host != nil {
			return
		}
		for index := len(cleanups) - 1; index >= 0; index-- {
			_ = cleanups[index]()
		}
	}()
	required := make(map[string]struct{}, len(options.Required))
	for _, capability := range options.Required {
		if capability == "" {
			return nil, errors.New("standard-library host requires a non-empty capability")
		}
		if _, duplicate := required[capability]; duplicate {
			return nil, fmt.Errorf("standard-library capability %q is required more than once", capability)
		}
		required[capability] = struct{}{}
	}
	providers := make([]rpc.Provider, 0, len(options.Providers))
	provided := make(map[string]struct{}, len(options.Providers))
	for _, provider := range options.Providers {
		if provider.Capability == "" || provider.RPC == nil {
			return nil, errors.New("standard-library host provider is incomplete")
		}
		if _, duplicate := provided[provider.Capability]; duplicate {
			return nil, fmt.Errorf("standard-library capability %q has multiple providers", provider.Capability)
		}
		provided[provider.Capability] = struct{}{}
		providers = append(providers, provider.RPC)
	}
	for capability := range required {
		if _, exists := provided[capability]; !exists {
			return nil, fmt.Errorf("standard-library capability %q has no provider", capability)
		}
	}
	rpcHost, err := rpc.NewHost(rpc.HostOptions{
		Providers: providers, Fallback: options.Fallback, PublishProvider: options.PublishProvider, Limits: options.Limits,
	})
	if err != nil {
		return nil, err
	}
	capabilities := make([]string, 0, len(provided))
	for capability := range provided {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return &Host{rpcHost: rpcHost, cleanups: cleanups, capabilities: capabilities, done: make(chan struct{})}, nil
}

// HostCapabilities returns the exact capability transaction installed by h.
func (h *Host) HostCapabilities() []string {
	if h == nil {
		return nil
	}
	return append([]string(nil), h.capabilities...)
}

// Open creates one FFI session owned by a Mini-Go runtime instance.
func (h *Host) Open(ctx context.Context) (ffi.Session, error) {
	if h == nil || h.rpcHost == nil {
		return nil, errors.New("standard-library host is closed")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errors.New("standard-library host is closed")
	}
	return h.rpcHost.Open(ctx)
}

// Close first stops RPC leases, then releases owned backends in reverse order.
func (h *Host) Close() error {
	return h.Shutdown(context.Background())
}

// Shutdown stops sessions before releasing owned backends. The context limits
// the wait; cleanup continues and subsequent calls wait for the same outcome.
func (h *Host) Shutdown(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.once.Do(func() {
		h.mu.Lock()
		h.closed = true
		if h.done == nil {
			h.done = make(chan struct{})
		}
		h.mu.Unlock()
		go func() {
			if h.rpcHost != nil {
				h.err = h.rpcHost.Close()
			}
			for index := len(h.cleanups) - 1; index >= 0; index-- {
				h.err = errors.Join(h.err, h.cleanups[index]())
			}
			h.cleanups = nil
			close(h.done)
		}()
	})
	select {
	case <-h.done:
		return h.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
