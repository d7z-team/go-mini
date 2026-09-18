package ffi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type blockingMuxSession struct {
	started chan struct{}
	release chan struct{}
	closes  atomic.Int32
	err     error
}

func (s *blockingMuxSession) Open(context.Context) (Session, error) {
	return s, nil
}

func (*blockingMuxSession) Start(context.Context, Request, Completion) (Call, error) {
	return nil, nil
}

func (s *blockingMuxSession) Shutdown(context.Context) error {
	s.closes.Add(1)
	close(s.started)
	<-s.release
	return s.err
}

func TestMuxShutdownRetainsCleanupAfterCanceledWait(t *testing.T) {
	child := &blockingMuxSession{started: make(chan struct{}), release: make(chan struct{}), err: errors.New("close failed")}
	bridge, err := NewMux(MuxRoute{Route: "resource", Bridge: child})
	if err != nil {
		t.Fatal(err)
	}
	session, err := bridge.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(func() { close(child.release) })
	t.Cleanup(func() {
		release()
		_ = session.Shutdown(context.Background())
	})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-child.started
	if err := session.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("second wait = %v", err)
	}
	release()
	for range 2 {
		if err := session.Shutdown(context.Background()); !errors.Is(err, child.err) {
			t.Fatalf("final outcome = %v", err)
		}
	}
	if child.closes.Load() != 1 {
		t.Fatalf("closes = %d", child.closes.Load())
	}
}

type muxTestBridge struct {
	capabilities []string
	openErr      error
	opened       int
	shutdown     int
	name         string
	closeOrder   *[]string
}

func (b *muxTestBridge) HostCapabilities() []string {
	return append([]string(nil), b.capabilities...)
}

func (b *muxTestBridge) Open(context.Context) (Session, error) {
	if b.openErr != nil {
		return nil, b.openErr
	}
	b.opened++
	return &muxTestSession{owner: b}, nil
}

type muxTestSession struct{ owner *muxTestBridge }

func (s *muxTestSession) Start(_ context.Context, request Request, complete Completion) (Call, error) {
	complete(Result{Payload: append([]byte(nil), request.Payload...)})
	return CancelFunc(func() {}), nil
}

func (s *muxTestSession) Shutdown(context.Context) error {
	s.owner.shutdown++
	if s.owner.closeOrder != nil {
		*s.owner.closeOrder = append(*s.owner.closeOrder, s.owner.name)
	}
	return nil
}

func TestMuxSessionOrderAndIsolation(t *testing.T) {
	for _, failOpen := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail_open=%t", failOpen), func(t *testing.T) {
			var order []string
			routes := []MuxRoute{
				{Route: "first", Bridge: &muxTestBridge{name: "first", closeOrder: &order}},
				{Route: "second", Bridge: &muxTestBridge{name: "second", closeOrder: &order}},
			}
			wantErr := errors.New("open failed")
			if failOpen {
				routes = append(routes, MuxRoute{Route: "third", Bridge: &muxTestBridge{openErr: wantErr}})
			}
			bridge, err := NewMux(routes...)
			if err != nil {
				t.Fatal(err)
			}
			// The factory owns its configuration independently of caller storage.
			routes[0] = MuxRoute{}
			first, err := bridge.Open(context.Background())
			if failOpen {
				if !errors.Is(err, wantErr) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = first.Shutdown(context.Background()) })
				second, err := bridge.Open(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = second.Shutdown(context.Background()) })
				if err := first.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				for _, route := range []string{"first", "second"} {
					var result Result
					if _, err := second.Start(context.Background(), Request{Route: route, Payload: []byte(route)}, func(r Result) { result = r }); err != nil {
						t.Fatal(err)
					}
					if string(result.Payload) != route {
						t.Fatalf("route %q returned %q", route, result.Payload)
					}
				}
			}
			if fmt.Sprint(order) != "[second first]" {
				t.Fatalf("cleanup order = %v", order)
			}
		})
	}
}

func BenchmarkMuxSessionLifecycle(b *testing.B) {
	bridge, err := NewMux(
		MuxRoute{Route: "first", Bridge: &muxTestBridge{}},
		MuxRoute{Route: "second", Bridge: &muxTestBridge{}},
		MuxRoute{Route: "third", Bridge: &muxTestBridge{}},
	)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		session, err := bridge.Open(context.Background())
		if err != nil {
			b.Fatal(err)
		}
		if err := session.Shutdown(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMuxRoutesExactCallsAndOwnsChildSessions(t *testing.T) {
	left := &muxTestBridge{capabilities: []string{"filesystem", "console"}}
	right := &muxTestBridge{capabilities: []string{"console", "entropy"}}
	bridge, err := NewMux(
		MuxRoute{Route: "left", Bridge: left},
		MuxRoute{Route: "right", Bridge: right},
	)
	if err != nil {
		t.Fatal(err)
	}
	capable := bridge.(CapabilityBridge)
	capabilities := capable.HostCapabilities()
	if got, want := fmt.Sprint(capabilities), "[console entropy filesystem]"; got != want {
		t.Fatalf("capabilities = %s, want %s", got, want)
	}
	capabilities[0] = "changed"
	if capable.HostCapabilities()[0] != "console" {
		t.Fatal("caller mutated mux capabilities")
	}
	session, err := bridge.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if _, err := session.Start(context.Background(), Request{Route: "right", Payload: []byte("ok")}, func(value Result) { result = value }); err != nil {
		t.Fatal(err)
	}
	if string(result.Payload) != "ok" {
		t.Fatalf("payload = %q", result.Payload)
	}
	if _, err := session.Start(context.Background(), Request{Route: "missing"}, func(Result) {}); !errors.Is(err, ErrRouteUnavailable) {
		t.Fatalf("missing route = %v", err)
	}
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Start(context.Background(), Request{Route: "left"}, func(Result) {}); err == nil || errors.Is(err, ErrRouteUnavailable) {
		t.Fatalf("closed session = %v", err)
	}
	if left.opened != 1 || right.opened != 1 || left.shutdown != 1 || right.shutdown != 1 {
		t.Fatalf("child lifecycle = left(%d,%d), right(%d,%d)", left.opened, left.shutdown, right.opened, right.shutdown)
	}
}

func TestMuxRejectsDuplicatesAndRollsBackOpen(t *testing.T) {
	bridge := &muxTestBridge{}
	if _, err := NewMux(MuxRoute{Route: "same", Bridge: bridge}, MuxRoute{Route: "same", Bridge: bridge}); err == nil {
		t.Fatal("duplicate route was accepted")
	}
	if _, err := NewMux(MuxRoute{Route: "invalid", Bridge: &muxTestBridge{capabilities: []string{" invalid"}}}); err == nil {
		t.Fatal("invalid capability was accepted")
	}
	if _, err := NewMux(MuxRoute{Route: "duplicate", Bridge: &muxTestBridge{capabilities: []string{"console", "console"}}}); err == nil {
		t.Fatal("duplicate capability from one bridge was accepted")
	}
	first := &muxTestBridge{}
	failed := errors.New("open failed")
	mux, err := NewMux(MuxRoute{Route: "first", Bridge: first}, MuxRoute{Route: "second", Bridge: &muxTestBridge{openErr: failed}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mux.Open(context.Background()); !errors.Is(err, failed) {
		t.Fatalf("Open error = %v", err)
	}
	if first.opened != 1 || first.shutdown != 1 {
		t.Fatalf("rollback lifecycle = (%d,%d)", first.opened, first.shutdown)
	}
}
