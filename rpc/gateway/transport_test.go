package gateway

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/d7z-team/mini-go/rpc"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

func unixHTTPClient(socketPath string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
}

func TestParseWebSocketAddresses(t *testing.T) {
	tests := []struct {
		value string
		want  Address
	}{
		{value: "ws://127.0.0.1:7231", want: Address{Scheme: "ws", Host: "127.0.0.1:7231", Path: "/rpc"}},
		{value: "wss://gateway.example/service", want: Address{Scheme: "wss", Host: "gateway.example", Path: "/service"}},
		{value: "ws+unix:///tmp/minigo.sock", want: Address{Scheme: "ws+unix", Host: "minigo.local", Path: "/rpc", SocketPath: "/tmp/minigo.sock"}},
	}
	for _, test := range tests {
		got, err := ParseAddress(test.value)
		if err != nil || got != test.want {
			t.Fatalf("ParseAddress(%q) = %#v, %v; want %#v", test.value, got, err, test.want)
		}
	}
	for _, value := range []string{"", "ws://", "ws+unix://relative.sock"} {
		if _, err := ParseAddress(value); err == nil {
			t.Fatalf("ParseAddress(%q) succeeded", value)
		}
	}
}

func TestWebSocketServerShutdownClosesEndpoint(t *testing.T) {
	gateway, _ := catalogGateway(t)
	address := "ws+unix://" + filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(gateway, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, listener, server) }()
	endpoint, err := Dial(context.Background(), address, DialOptions{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case <-endpoint.Done():
	case <-time.After(time.Second):
		t.Fatal("Endpoint remained active after WebSocket Server shutdown")
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestDialSecureWebSocketGateway(t *testing.T) {
	gateway, _ := catalogGateway(t)
	server, err := NewServer(gateway, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	address := "wss" + strings.TrimPrefix(httpServer.URL, "https") + "/rpc"
	endpoint, err := Dial(context.Background(), address, DialOptions{HTTPClient: httpServer.Client()})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(context.Background(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDialWebSocketGatewayHonorsContext(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	address := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/rpc"
	if _, err := Dial(ctx, address, DialOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Dial error = %v", err)
	}
}

func TestWebSocketServerRequiresBinaryEndpointMessages(t *testing.T) {
	gateway, _ := catalogGateway(t)
	address := "ws+unix://" + filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(gateway, ServerOptions{Endpoint: rpc.EndpointOptions{Limits: rpc.Limits{MaxFrameBytes: 1024}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, listener, server) }()

	transport := unixHTTPClient(listener.Address.SocketPath)
	conn, _, err := websocket.Dial(context.Background(), "ws://minigo.local/rpc", &websocket.DialOptions{
		HTTPClient:   transport,
		Subprotocols: []string{rpc.EndpointProtocol},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, []byte("not an Endpoint frame")); err != nil {
		cancel()
		t.Fatal(err)
	}
	readCtx, stopRead := context.WithTimeout(context.Background(), time.Second)
	defer stopRead()
	for err == nil {
		_, _, err = conn.Read(readCtx)
	}
	if err == nil || readCtx.Err() != nil {
		cancel()
		_ = conn.CloseNow()
		<-served
		t.Fatalf("text message did not close the WebSocket: %v", err)
	}
	_ = conn.CloseNow()

	withoutProtocol, _, err := websocket.Dial(context.Background(), "ws://minigo.local/rpc", &websocket.DialOptions{HTTPClient: transport})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	protocolCtx, stopProtocolRead := context.WithTimeout(context.Background(), time.Second)
	_, _, err = withoutProtocol.Read(protocolCtx)
	protocolContextErr := protocolCtx.Err()
	stopProtocolRead()
	_ = withoutProtocol.CloseNow()
	if err == nil || protocolContextErr != nil {
		cancel()
		<-served
		t.Fatalf("missing Endpoint subprotocol did not close the WebSocket: %v", err)
	}

	oversized, _, err := websocket.Dial(context.Background(), "ws://minigo.local/rpc", &websocket.DialOptions{
		HTTPClient:   transport,
		Subprotocols: []string{rpc.EndpointProtocol},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := oversized.Write(context.Background(), websocket.MessageBinary, make([]byte, 1025)); err != nil {
		cancel()
		t.Fatal(err)
	}
	limitCtx, stopLimitRead := context.WithTimeout(context.Background(), time.Second)
	err = nil
	for err == nil {
		_, _, err = oversized.Read(limitCtx)
	}
	limitContextErr := limitCtx.Err()
	stopLimitRead()
	_ = oversized.CloseNow()
	if err == nil || limitContextErr != nil {
		cancel()
		<-served
		t.Fatalf("oversized message did not close the WebSocket: %v", err)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketPreservesNilAndEmptyCollections(t *testing.T) {
	method := rpc.Method{
		ID:           "example/collections::Echo.RoundTrip",
		Service:      "example/collections::Echo",
		Name:         "RoundTrip",
		ContractHash: strings.Repeat("a", 64),
	}
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(_ context.Context, values []rpc.Value) ([]rpc.Value, error) {
		return values, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	router := rpcrouter.New(rpcrouter.Options{})
	if _, err := router.Register(provider, rpcrouter.RegistrationOptions{Name: "collections"}); err != nil {
		t.Fatal(err)
	}
	defer router.ForceShutdown(context.Background())

	address := "ws+unix://" + filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	limits := rpc.Limits{MaxFrameBytes: 256, MaxMessageBytes: 16 << 10, MaxInFlightBytes: 32 << 10}
	server, err := NewServer(router, ServerOptions{Endpoint: rpc.EndpointOptions{Limits: limits}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, listener, server) }()

	endpoint, err := Dial(context.Background(), address, DialOptions{Endpoint: rpc.EndpointOptions{Limits: limits}})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	routes, err := endpoint.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{
		Protocol: rpc.ContractProtocol,
		Methods:  []rpc.Method{method},
	}})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	values := []rpc.Value{
		{Type: "[]uint8"},
		{Type: "[]uint8", Data: []byte{}},
		{Type: "[]string"},
		{Type: "[]string", Data: []rpc.Value{}},
		{Type: "map[string]int64"},
		{Type: "map[string]int64", Data: []rpc.MapEntry{}},
		{Type: "[]uint8", Data: bytes.Repeat([]byte("large"), 1000)},
	}
	result, err := routes.Call(context.Background(), rpc.Call{Method: method, Arguments: values})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if result.Values[0].Data != nil || result.Values[2].Data != nil || result.Values[4].Data != nil {
		t.Fatalf("nil collections became non-nil: %#v", result.Values)
	}
	emptyBytes, ok := result.Values[1].Data.([]byte)
	if !ok || emptyBytes == nil || len(emptyBytes) != 0 {
		t.Fatalf("empty bytes = %#v", result.Values[1].Data)
	}
	items, ok := result.Values[3].Data.([]rpc.Value)
	if !ok || items == nil || len(items) != 0 {
		t.Fatalf("empty slice = %#v", result.Values[3].Data)
	}
	entries, ok := result.Values[5].Data.([]rpc.MapEntry)
	if !ok || entries == nil || len(entries) != 0 {
		t.Fatalf("empty map = %#v", result.Values[5].Data)
	}
	large, ok := result.Values[6].Data.([]byte)
	if !ok || !bytes.Equal(large, values[6].Data.([]byte)) {
		t.Fatalf("fragmented bytes = %#v", result.Values[6].Data)
	}
	if err := result.Accept(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}
