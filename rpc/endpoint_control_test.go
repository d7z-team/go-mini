package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestEndpointSharedFragmentAssembly(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/wire/fragments.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string
		Fragments []string
		Messages  []string
		ErrorAt   *int `json:"error_at"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	limits := normalizeLimits(Limits{MaxFrameBytes: 256})
	for _, scenario := range cases {
		t.Run(scenario.Name, func(t *testing.T) {
			var assembly endpointAssembly
			var previous uint64
			for index, encoded := range scenario.Fragments {
				payload, err := hex.DecodeString(encoded)
				if err != nil {
					t.Fatal(err)
				}
				fragment, err := decodeEndpointFragment(payload, limits)
				var message []byte
				if err == nil {
					message, err = assembly.append(fragment, &previous)
				}
				if scenario.ErrorAt != nil && index == *scenario.ErrorAt {
					if err == nil {
						t.Fatal("invalid fragment sequence accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if hex.EncodeToString(message) != scenario.Messages[index] {
					t.Fatalf("fragment %d output = %x", index, message)
				}
			}
		})
	}
}

type controlInterleaveConn struct {
	MessageConn
	limits    Limits
	started   chan struct{}
	resume    chan struct{}
	once      sync.Once
	mu        sync.Mutex
	fragments []endpointFragment
}

func (c *controlInterleaveConn) Write(ctx context.Context, data []byte) error {
	fragment, err := decodeEndpointFragment(data, c.limits)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.fragments = append(c.fragments, endpointFragment{messageID: fragment.messageID, total: fragment.total, offset: fragment.offset})
	c.mu.Unlock()
	if err := c.MessageConn.Write(ctx, data); err != nil {
		return err
	}
	if fragment.messageID != 0 && fragment.total > 4096 && fragment.offset == 0 {
		c.once.Do(func() {
			close(c.started)
			select {
			case <-c.resume:
			case <-ctx.Done():
			}
		})
	}
	return ctx.Err()
}

func TestEndpointControlInterleavesLargeRequestFragments(t *testing.T) {
	method := Method{ID: "example::Service.Echo", Service: "example::Service", Name: "Echo", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(_ context.Context, arguments []Value) ([]Value, error) { return arguments, nil })
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	limits := normalizeLimits(Limits{MaxFrameBytes: 256})
	a, b := newTestMessagePipe()
	conn := &controlInterleaveConn{MessageConn: a, limits: limits, started: make(chan struct{}), resume: make(chan struct{})}
	resume := sync.OnceFunc(func() { close(conn.resume) })
	client, err := OpenEndpoint(conn, EndpointServices{}, EndpointOptions{Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	server, err := OpenEndpoint(b, EndpointServices{Binder: binder}, EndpointOptions{Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		resume()
		_ = client.Close()
		_ = server.Close()
		_ = client.Shutdown(context.Background())
		_ = server.Shutdown(context.Background())
	})
	routes, err := client.Bind(t.Context(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("fragment", 4096)
	called := make(chan error, 1)
	go func() {
		result, err := routes.Call(t.Context(), Call{Method: method, Arguments: []Value{{Type: "string", Data: value}}})
		if err == nil {
			if len(result.Values) != 1 || result.Values[0].Data != value {
				t.Error("fragmented call changed payload")
			}
			err = result.Accept(t.Context())
		}
		called <- err
	}()
	<-conn.started
	// The completed BIND operation is a valid historical cancellation target.
	// Its control frame must not disturb the in-progress CALL assembly.
	for range 8 {
		if err := client.writeContext(t.Context(), endpointFrame{Kind: "cancel", Origin: client.origin, TargetID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	resume()
	if err := <-called; err != nil {
		t.Fatal(err)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	var dataID uint64
	interleaved := false
	burst := 0
	for _, fragment := range conn.fragments {
		if dataID == 0 && fragment.total > 4096 {
			dataID = fragment.messageID
			continue
		}
		if dataID != 0 && fragment.messageID == 0 {
			interleaved = true
			burst++
			continue
		}
		if interleaved && fragment.messageID == dataID {
			if burst > 4 {
				t.Fatalf("control burst starved data: %d", burst)
			}
			return
		}
	}
	t.Fatal("control did not pass between data fragments")
}
