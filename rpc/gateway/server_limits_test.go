package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/d7z-team/mini-go/rpc"
)

func TestDrainTracksAcceptedHandshakeAndSharedShutdown(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "shutdown"}[force], func(t *testing.T) {
			gateway, _ := catalogGateway(t)
			started, release := make(chan struct{}), make(chan struct{})
			server, err := NewServer(gateway, ServerOptions{PeerInfo: func(*http.Request) (rpc.PeerInfo, error) {
				close(started)
				<-release
				return rpc.PeerInfo{}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			httpServer := httptest.NewServer(server)
			defer httpServer.Close()
			defer server.Shutdown(context.Background())
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			connected := make(chan error, 1)
			go func() {
				conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http"), &websocket.DialOptions{Subprotocols: []string{rpc.EndpointProtocol}})
				if conn != nil {
					_ = conn.CloseNow()
				}
				connected <- err
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			server.BeginDrain()
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatal(recorder.Code)
			}
			if force {
				wait, stop := context.WithCancel(ctx)
				stop()
				for range 64 {
					if err := server.Shutdown(wait); !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				}
				select {
				case <-server.shutdownDone:
					t.Fatal("handshake owner lost")
				default:
				}
			}
			close(release)
			if err := <-connected; err != nil {
				t.Fatal(err)
			}
			if err := server.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if len(server.connections) != 0 {
				t.Fatal("connection slot retained after shutdown")
			}
		})
	}
}

func TestServerRejectsSaturatedHandshakeBeforeUpgrade(t *testing.T) {
	gateway, _ := catalogGateway(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var authenticateCalls atomic.Int32
	server, err := NewServer(gateway, ServerOptions{
		MaxConnections: 1,
		PeerInfo: func(*http.Request) (rpc.PeerInfo, error) {
			if authenticateCalls.Add(1) == 1 {
				close(started)
				<-release
			}
			return rpc.PeerInfo{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	address := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/rpc"
	firstDone := make(chan struct {
		conn *websocket.Conn
		err  error
	}, 1)
	go func() {
		conn, _, dialErr := websocket.Dial(context.Background(), address, &websocket.DialOptions{Subprotocols: []string{rpc.EndpointProtocol}})
		firstDone <- struct {
			conn *websocket.Conn
			err  error
		}{conn: conn, err: dialErr}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter handshake")
	}
	secondCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(secondCtx, address, &websocket.DialOptions{Subprotocols: []string{rpc.EndpointProtocol}}); err == nil {
		t.Fatal("saturated Gateway accepted a second handshake")
	}
	if calls := authenticateCalls.Load(); calls != 1 {
		t.Fatalf("saturated request reached authentication: calls = %d", calls)
	}
	close(release)
	first := <-firstDone
	if first.err != nil {
		t.Fatal(first.err)
	}
	_ = first.conn.CloseNow()
	deadline := time.Now().Add(time.Second)
	for len(server.connections) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(server.connections) != 0 {
		t.Fatal("Gateway did not release the connection slot after Endpoint cleanup")
	}
	second, _, err := websocket.Dial(context.Background(), address, &websocket.DialOptions{Subprotocols: []string{rpc.EndpointProtocol}})
	if err != nil {
		t.Fatalf("Gateway did not admit a connection after cleanup: %v", err)
	}
	_ = second.CloseNow()
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestServerRejectsNegativeConnectionLimit(t *testing.T) {
	gateway, _ := catalogGateway(t)
	if _, err := NewServer(gateway, ServerOptions{MaxConnections: -1}); err == nil {
		t.Fatal("negative Gateway connection limit was accepted")
	}
}

func TestServerUsesBoundedDefaultConnectionLimit(t *testing.T) {
	gateway, _ := catalogGateway(t)
	server, err := NewServer(gateway, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cap(server.connections); got != defaultMaxConnections {
		t.Fatalf("default Gateway connection limit = %d, want %d", got, defaultMaxConnections)
	}
}

type cleanupFailureProvider struct {
	contract rpc.Contract
	err      error
}

func (provider *cleanupFailureProvider) RPCContract() rpc.Contract { return provider.contract }
func (provider *cleanupFailureProvider) BindRPC(context.Context, rpc.BindRequest) (rpc.ProviderLease, error) {
	return provider, nil
}

func (*cleanupFailureProvider) Invoke(context.Context, rpc.Method, []rpc.Value) (*rpc.ProviderResult, error) {
	return &rpc.ProviderResult{}, nil
}

func (provider *cleanupFailureProvider) Close() error { return provider.err }

func TestServerShutdownRetainsEndpointCleanupFailure(t *testing.T) {
	want := errors.New("provider cleanup failed")
	contract := rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{{ID: "drain::Service.Call", Service: "drain::Service", Name: "Call", ContractHash: testContractHash}}}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, &cleanupFailureProvider{contract: contract, err: want})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(binder, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	endpoint, err := Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http"), DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	if _, err := endpoint.Bind(ctx, rpc.BindRequest{Contract: contract}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := server.Shutdown(ctx); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	if len(server.connections) != 0 {
		t.Fatal("cleanup did not release connection slot")
	}
}
