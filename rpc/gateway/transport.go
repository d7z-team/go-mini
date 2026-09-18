package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/coder/websocket"

	"github.com/d7z-team/mini-go/rpc"
)

const defaultWebSocketPath = "/rpc"

// Address is one WebSocket Gateway listen or dial target.
type Address struct {
	Scheme     string
	Host       string
	Path       string
	SocketPath string
}

// ParseAddress accepts WebSocket addresses over TCP, TLS, or a Unix socket.
func ParseAddress(value string) (Address, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Address{}, errors.New("Gateway address is invalid")
	}
	switch parsed.Scheme {
	case "ws", "wss":
		if parsed.Host == "" {
			return Address{}, errors.New("Gateway WebSocket address has no host")
		}
		requestPath := parsed.Path
		if requestPath == "" {
			requestPath = defaultWebSocketPath
		}
		return Address{Scheme: parsed.Scheme, Host: parsed.Host, Path: requestPath}, nil
	case "ws+unix":
		if parsed.Host != "" || !path.IsAbs(parsed.Path) {
			return Address{}, errors.New("Gateway WebSocket Unix address must contain an absolute socket path")
		}
		return Address{Scheme: parsed.Scheme, Host: "minigo.local", Path: defaultWebSocketPath, SocketPath: parsed.Path}, nil
	default:
		return Address{}, errors.New("Gateway address must use ws://, wss://, or ws+unix://")
	}
}

// Listener owns the carrier used by one WebSocket Gateway HTTP server.
type Listener struct {
	net.Listener
	Address Address
}

// Listen opens the carrier named by a WebSocket Gateway address.
func Listen(address string) (*Listener, error) {
	target, err := ParseAddress(address)
	if err != nil {
		return nil, err
	}
	network, endpoint := "tcp", target.Host
	if target.Scheme == "ws+unix" {
		network, endpoint = "unix", target.SocketPath
	}
	listener, err := net.Listen(network, endpoint)
	if err != nil {
		return nil, fmt.Errorf("listen on Gateway %s: %w", address, err)
	}
	return &Listener{Listener: listener, Address: target}, nil
}

// DialOptions configures a WebSocket Gateway connection.
type DialOptions struct {
	Endpoint   rpc.EndpointOptions
	Services   rpc.EndpointServices
	HTTPClient *http.Client
	Header     http.Header
	Path       string
}

type websocketMessageConn struct{ conn *websocket.Conn }

func (conn websocketMessageConn) Read(ctx context.Context) ([]byte, error) {
	kind, message, err := conn.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if kind != websocket.MessageBinary {
		return nil, rpc.StatusError{Code: rpc.CodeProtocol, Message: "Gateway WebSocket message is not binary"}
	}
	return message, nil
}

func (conn websocketMessageConn) Write(ctx context.Context, message []byte) error {
	return conn.conn.Write(ctx, websocket.MessageBinary, message)
}

func (conn websocketMessageConn) Close() error { return conn.conn.CloseNow() }

// Dial opens one symmetric Endpoint to a WebSocket Gateway.
func Dial(ctx context.Context, address string, options DialOptions) (*rpc.Endpoint, error) {
	target, err := ParseAddress(address)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestURL := (&url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path}).String()
	if options.Path != "" {
		target.Path = options.Path
		requestURL = (&url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path}).String()
	}
	httpClient := options.HTTPClient
	var unixTransport *http.Transport
	if target.Scheme == "ws+unix" {
		requestURL = (&url.URL{Scheme: "ws", Host: target.Host, Path: target.Path}).String()
		unixTransport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", target.SocketPath)
		}}
		httpClient = &http.Client{Transport: unixTransport}
		if options.HTTPClient != nil {
			*httpClient = *options.HTTPClient
			httpClient.Transport = unixTransport
		}
	}
	conn, response, err := websocket.Dial(ctx, requestURL, &websocket.DialOptions{
		HTTPClient:      httpClient,
		HTTPHeader:      options.Header.Clone(),
		Subprotocols:    []string{rpc.EndpointProtocol},
		CompressionMode: websocket.CompressionDisabled,
	})
	if unixTransport != nil {
		unixTransport.CloseIdleConnections()
	}
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("dial Gateway %s: HTTP %s: %w", address, response.Status, err)
		}
		return nil, fmt.Errorf("dial Gateway %s: %w", address, err)
	}
	if conn.Subprotocol() != rpc.EndpointProtocol {
		_ = conn.Close(websocket.StatusProtocolError, "WebSocket subprotocol mismatch")
		return nil, errors.New("Gateway WebSocket subprotocol mismatch")
	}
	conn.SetReadLimit(int64(rpc.NormalizeLimits(options.Endpoint.Limits).MaxFrameBytes))
	endpoint, err := rpc.OpenEndpoint(websocketMessageConn{conn: conn}, options.Services, options.Endpoint)
	if err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	if err := endpoint.Ready(ctx); err != nil {
		_ = endpoint.Close()
		return nil, err
	}
	return endpoint, nil
}
