package ffi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MuxRoute binds one exact FFI route to a Bridge.
type MuxRoute struct {
	Route  string
	Bridge Bridge
}

// NewMux composes independent host capabilities behind exact route names.
func NewMux(routes ...MuxRoute) (Bridge, error) {
	seen := make(map[string]struct{}, len(routes))
	bindings := make([]MuxRoute, 0, len(routes))
	capabilitySet := make(map[string]struct{})
	for _, route := range routes {
		name := strings.TrimSpace(route.Route)
		if name == "" || route.Bridge == nil {
			return nil, errors.New("FFI mux route and bridge are required")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate FFI route %q", name)
		}
		seen[name] = struct{}{}
		bindings = append(bindings, MuxRoute{Route: name, Bridge: route.Bridge})
		if bridge, ok := route.Bridge.(CapabilityBridge); ok {
			routeCapabilities := make(map[string]struct{})
			for _, capability := range bridge.HostCapabilities() {
				if strings.TrimSpace(capability) != capability || capability == "" {
					return nil, fmt.Errorf("FFI route %q advertises invalid host capability %q", name, capability)
				}
				if _, duplicate := routeCapabilities[capability]; duplicate {
					return nil, fmt.Errorf("FFI route %q advertises duplicate host capability %q", name, capability)
				}
				routeCapabilities[capability] = struct{}{}
				capabilitySet[capability] = struct{}{}
			}
		}
	}
	if len(bindings) == 0 {
		return nil, errors.New("FFI mux requires at least one route")
	}
	capabilities := make([]string, 0, len(capabilitySet))
	for capability := range capabilitySet {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return &mux{routes: bindings, capabilities: capabilities}, nil
}

type mux struct {
	routes       []MuxRoute
	capabilities []string
}

func (m *mux) HostCapabilities() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.capabilities...)
}

func (m *mux) Open(ctx context.Context) (Session, error) {
	sessions := make(map[string]Session, len(m.routes))
	opened := make([]Session, 0, len(m.routes))
	for _, route := range m.routes {
		session, err := route.Bridge.Open(ctx)
		if err != nil || session == nil {
			if err == nil {
				err = fmt.Errorf("FFI route %q returned a nil session", route.Route)
			}
			for index := len(opened) - 1; index >= 0; index-- {
				err = errors.Join(err, opened[index].Shutdown(context.Background()))
			}
			return nil, err
		}
		sessions[route.Route] = session
		opened = append(opened, session)
	}
	return &muxSession{sessions: sessions, opened: opened, shutdownDone: make(chan struct{})}, nil
}

type muxSession struct {
	mu           sync.RWMutex
	sessions     map[string]Session
	opened       []Session
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error
}

func (s *muxSession) Start(ctx context.Context, request Request, complete Completion) (Call, error) {
	s.mu.RLock()
	closed := s.sessions == nil
	session := s.sessions[request.Route]
	s.mu.RUnlock()
	if closed {
		return nil, errors.New("FFI mux session is closed")
	}
	if session == nil {
		return nil, fmt.Errorf("%w: %s", ErrRouteUnavailable, request.Route)
	}
	return session.Start(ctx, request, complete)
}

func (s *muxSession) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		sessions := s.opened
		s.sessions = nil
		s.opened = nil
		s.mu.Unlock()
		go func() {
			for index := len(sessions) - 1; index >= 0; index-- {
				s.shutdownErr = errors.Join(s.shutdownErr, sessions[index].Shutdown(context.Background()))
			}
			close(s.shutdownDone)
		}()
	})
	select {
	case <-s.shutdownDone:
		return s.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
