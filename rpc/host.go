package rpc

import (
	"context"
	"errors"
	"sync"

	"github.com/d7z-team/mini-go/ffi"
)

// HostOptions composes local providers, an optional fallback binder, and
// guest-provider publication on the fixed MRPC FFI route.
type HostOptions struct {
	// MaxSessions bounds live sessions, including sessions still cleaning up.
	// Zero selects 1024.
	MaxSessions     int
	Providers       []Provider
	Fallback        Binder
	PublishProvider PublishProviderFunc
	Limits          Limits
	Interceptors    []UnaryInterceptor
}

// Host is a shareable MRPC FFI session factory. Each opened session owns only the route,
// provider, and resource leases established by one Mini-Go instance.
type Host struct {
	config sessionConfig

	mu           sync.Mutex
	closed       bool
	maxSessions  int
	reservations int
	openers      sync.WaitGroup
	sessions     map[*ffiSession]struct{}
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error
}

// NewHost creates the transport-neutral MRPC extension used by VM instances.
// Empty options install an empty local binder; binding returns unimplemented.
func NewHost(options HostOptions) (*Host, error) {
	if options.MaxSessions < 0 {
		return nil, errors.New("MRPC host session limit must be positive")
	}
	if options.MaxSessions == 0 {
		options.MaxSessions = 1024
	}
	binder := options.Fallback
	if len(options.Providers) != 0 || binder == nil {
		local, err := NewLocalBinder(LocalBinderOptions{Limits: options.Limits}, options.Providers...)
		if err != nil {
			return nil, err
		}
		binder = local
		if options.Fallback != nil {
			binder = fallbackBinder{primary: local, fallback: options.Fallback}
		}
	}
	return &Host{
		maxSessions: options.MaxSessions,
		config: sessionConfig{
			Binder: binder, Limits: options.Limits, PublishProvider: options.PublishProvider,
			Interceptors: append([]UnaryInterceptor(nil), options.Interceptors...),
		},
		sessions:     make(map[*ffiSession]struct{}),
		shutdownDone: make(chan struct{}),
	}, nil
}

// Open creates one FFI session owned by a Mini-Go instance.
func (h *Host) Open(ctx context.Context) (ffi.Session, error) {
	if h == nil {
		return nil, errors.New("nil MRPC host")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC host is closed"}
	}
	if len(h.sessions)+h.reservations >= h.maxSessions {
		h.mu.Unlock()
		return nil, StatusError{Code: CodeResourceExhausted, Message: "MRPC host session limit exceeded"}
	}
	h.reservations++
	h.openers.Add(1)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.reservations--
		h.mu.Unlock()
		h.openers.Done()
	}()

	session, err := newFFISession(h.config)
	if err != nil {
		return nil, err
	}
	session.onShutdown = h.removeSession
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = session.Shutdown(context.Background())
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC host is closed"}
	}
	if err := ctx.Err(); err != nil {
		h.mu.Unlock()
		_ = session.Shutdown(context.Background())
		return nil, statusError(err)
	}
	h.sessions[session] = struct{}{}
	h.mu.Unlock()
	return session, nil
}

func (h *Host) removeSession(session *ffiSession) {
	h.mu.Lock()
	delete(h.sessions, session)
	h.mu.Unlock()
}

// Shutdown rejects new sessions and waits for every existing session to clean
// up. The context bounds only the caller's wait; cleanup continues.
func (h *Host) Shutdown(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.shutdownOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		sessions := make([]*ffiSession, 0, len(h.sessions))
		for session := range h.sessions {
			sessions = append(sessions, session)
		}
		h.mu.Unlock()
		go func() {
			h.openers.Wait()
			var cleanup sync.WaitGroup
			errorsBySession := make([]error, len(sessions))
			for index, session := range sessions {
				cleanup.Add(1)
				go func() {
					defer cleanup.Done()
					errorsBySession[index] = session.Shutdown(context.Background())
				}()
			}
			cleanup.Wait()
			h.mu.Lock()
			h.shutdownErr = errors.Join(errorsBySession...)
			close(h.shutdownDone)
			h.mu.Unlock()
		}()
	})
	select {
	case <-h.shutdownDone:
		h.mu.Lock()
		err := h.shutdownErr
		h.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close shuts down the host without a caller deadline.
func (h *Host) Close() error { return h.Shutdown(context.Background()) }

type fallbackBinder struct {
	primary  Binder
	fallback Binder
}

func (b fallbackBinder) Bind(ctx context.Context, request BindRequest) (*RouteSet, error) {
	routes, err := b.primary.Bind(ctx, request)
	if err == nil || b.fallback == nil {
		return routes, err
	}
	if code, _ := CodeOf(err); code != CodeUnimplemented {
		return nil, err
	}
	return b.fallback.Bind(ctx, request)
}
