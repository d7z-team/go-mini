package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/d7z-team/mini-go/rpc"
)

const defaultMaxConnections = 4096

// PeerInfoFunc derives authenticated connection metadata from an upgraded HTTP request.
type PeerInfoFunc func(*http.Request) (rpc.PeerInfo, error)

// ServerOptions configures WebSocket Gateway sessions.
type ServerOptions struct {
	Endpoint rpc.EndpointOptions
	// MaxConnections bounds handshakes, active endpoints, and endpoints
	// whose asynchronous cleanup has not completed. Zero selects the default.
	MaxConnections  int
	PeerInfo        PeerInfoFunc
	Publications    *PublicationRegistry
	Drain           func(context.Context) error
	TLSConfig       *tls.Config
	ShutdownTimeout time.Duration
}

// Server accepts WebSocket sessions and owns their Endpoint lifetimes. It does
// not own the supplied Binder.
type Server struct {
	binder  rpc.Binder
	options ServerOptions

	mu           sync.Mutex
	closing      bool
	draining     bool
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	cleanupErr   error
	endpoints    map[*rpc.Endpoint]struct{}
	connections  chan struct{}
	handlers     sync.WaitGroup
}

// NewServer constructs a WebSocket Gateway HTTP handler.
func NewServer(binder rpc.Binder, options ServerOptions) (*Server, error) {
	if binder == nil {
		return nil, errors.New("Gateway Binder is required")
	}
	if options.MaxConnections < 0 {
		return nil, errors.New("Gateway MaxConnections cannot be negative")
	}
	if options.MaxConnections == 0 {
		options.MaxConnections = defaultMaxConnections
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = 30 * time.Second
	}
	return &Server{
		binder: binder, options: options,
		endpoints:    make(map[*rpc.Endpoint]struct{}),
		connections:  make(chan struct{}, options.MaxConnections),
		shutdownDone: make(chan struct{}),
	}, nil
}

// ServeHTTP upgrades one request and serves it until its Endpoint closes.
func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.serveSession(writer, request, false)
}

func (server *Server) servePublicationHTTP(writer http.ResponseWriter, request *http.Request) {
	server.serveSession(writer, request, true)
}

func (server *Server) serveSession(writer http.ResponseWriter, request *http.Request, publication bool) {
	server.mu.Lock()
	if server.draining || server.closing {
		server.mu.Unlock()
		http.Error(writer, "Gateway is shutting down", http.StatusServiceUnavailable)
		return
	}
	select {
	case server.connections <- struct{}{}:
	case <-request.Context().Done():
		server.mu.Unlock()
		return
	default:
		server.mu.Unlock()
		http.Error(writer, "Gateway connection limit exceeded", http.StatusServiceUnavailable)
		return
	}
	server.handlers.Add(1)
	server.mu.Unlock()
	defer func() {
		<-server.connections
		server.handlers.Done()
	}()

	peer := rpc.PeerInfo{}
	if server.options.PeerInfo != nil {
		var err error
		peer, err = server.options.PeerInfo(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
	}
	conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:    []string{rpc.EndpointProtocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	if conn.Subprotocol() != rpc.EndpointProtocol {
		_ = conn.Close(websocket.StatusProtocolError, "WebSocket subprotocol mismatch")
		return
	}
	conn.SetReadLimit(int64(rpc.NormalizeLimits(server.options.Endpoint.Limits).MaxFrameBytes))
	endpointOptions := server.options.Endpoint
	endpointOptions.Peer = peer
	endpoint, err := rpc.OpenEndpoint(websocketMessageConn{conn: conn}, rpc.EndpointServices{Binder: server.binder}, endpointOptions)
	if err != nil {
		_ = conn.CloseNow()
		return
	}
	defer func() {
		if err := endpoint.Wait(context.Background()); err != nil {
			server.mu.Lock()
			if server.cleanupErr == nil {
				server.cleanupErr = err
			}
			server.mu.Unlock()
		}
	}()
	server.mu.Lock()
	if server.closing {
		server.mu.Unlock()
		_ = endpoint.Close()
		return
	}
	server.endpoints[endpoint] = struct{}{}
	server.mu.Unlock()
	if publication {
		if server.options.Publications == nil {
			_ = endpoint.Close()
		} else {
			if err := server.options.Publications.accept(request.Context(), endpoint, peer); err != nil {
				_ = endpoint.Close()
			}
		}
	}
	// Publication acceptance may fail before it has waited for the endpoint's
	// coordinator. Keep the connection slot until all endpoint cleanup is done.
	<-endpoint.Done()
	server.mu.Lock()
	delete(server.endpoints, endpoint)
	server.mu.Unlock()
}

// BeginDrain rejects new sessions without interrupting accepted connections.
func (server *Server) BeginDrain() {
	if server == nil {
		return
	}
	server.mu.Lock()
	server.draining = true
	server.mu.Unlock()
}

// Shutdown rejects new sessions, closes active Endpoints, and waits for all
// upgraded handlers to return. The Binder remains usable by its owner.
func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	server.shutdownOnce.Do(func() {
		server.mu.Lock()
		server.closing = true
		if server.shutdownDone == nil {
			server.shutdownDone = make(chan struct{})
		}
		active := make([]*rpc.Endpoint, 0, len(server.endpoints))
		for endpoint := range server.endpoints {
			active = append(active, endpoint)
		}
		server.mu.Unlock()
		go func() {
			for _, endpoint := range active {
				_ = endpoint.Close()
			}
			server.handlers.Wait()
			close(server.shutdownDone)
		}()
	})
	select {
	case <-server.shutdownDone:
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Serve runs a WebSocket Gateway HTTP server on listener until ctx is canceled
// or the HTTP server fails.
func Serve(ctx context.Context, listener *Listener, server *Server) error {
	if listener == nil || server == nil {
		return errors.New("Gateway listener and Server are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mux := http.NewServeMux()
	mux.Handle(listener.Address.Path, server)
	if server.options.Publications != nil {
		mux.HandleFunc(publicationWebSocketPath, server.servePublicationHTTP)
	}
	httpServer := &http.Server{Handler: mux, TLSConfig: server.options.TLSConfig}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			server.BeginDrain()
			_ = httpServer.Close()
		case <-stop:
		}
	}()
	var serveErr error
	if listener.Address.Scheme == "wss" {
		if server.options.TLSConfig == nil {
			serveErr = errors.New("Gateway wss listener requires TLS configuration")
		} else {
			serveErr = httpServer.ServeTLS(listener, "", "")
		}
	} else {
		serveErr = httpServer.Serve(listener)
	}
	close(stop)
	server.BeginDrain()
	_ = listener.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), server.options.ShutdownTimeout)
	defer cancel()
	var shutdownErr error
	if server.options.Drain != nil {
		shutdownErr = server.options.Drain(shutdownCtx)
	}
	shutdownErr = errors.Join(shutdownErr, server.Shutdown(shutdownCtx))
	if errors.Is(serveErr, http.ErrServerClosed) || ctx.Err() != nil {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}
