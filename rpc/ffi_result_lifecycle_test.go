package rpc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFFIDecisionCancellationRetainsQuotaAndReleasesLateResult(t *testing.T) {
	started, finish, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	session := &ffiSession{limits: normalizeLimits(Limits{}), results: map[uint64]ffiPendingResult{
		1: {lease: 1, bytes: 7, result: &Result{
			accept:  func(bool) error { close(started); <-finish; return nil },
			release: func() error { close(released); return nil },
		}},
	}, resultBytes: 7, shutdownDone: make(chan struct{}), cancelCalls: func() {}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func() {
		close(finish)
		if err := session.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		select {
		case <-released:
		default:
			t.Error("late accepted resource was not released")
		}
		if session.decidingResults != 0 || session.resultBytes != 0 {
			t.Error("decision reservation retained after cleanup")
		}
	})
	done := make(chan error, 1)
	go func() { _, err := session.decideResult(ctx, 1, 1, true); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("decision ignored cancellation")
	}
	session.mu.Lock()
	count, bytes := session.decidingResults, session.resultBytes
	session.mu.Unlock()
	if count != 1 || bytes != 7 {
		t.Fatalf("decision lost reservation: count=%d bytes=%d", count, bytes)
	}
	select {
	case <-released:
		t.Fatal("resource released before confirmation finished")
	default:
	}
	if err := session.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-session.shutdownDone:
		t.Fatal("session stopped tracking the pending confirmation")
	default:
	}
}

func TestFFIDecisionCanceledBeforeAcceptDiscards(t *testing.T) {
	accepted := make(chan bool, 1)
	session := &ffiSession{results: map[uint64]ffiPendingResult{
		1: {lease: 1, result: &Result{accept: func(accept bool) error { accepted <- accept; return nil }}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := session.decideResult(ctx, 1, 1, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	session.workers.Wait()
	if <-accepted {
		t.Fatal("canceled confirmation accepted the result")
	}
}

func TestFFIDecisionTimeout(t *testing.T) {
	gate := make(chan struct{})
	session := &ffiSession{limits: normalizeLimits(Limits{}), results: map[uint64]ffiPendingResult{
		1: {lease: 1, result: &Result{accept: func(bool) error { <-gate; return nil }}},
	}}
	defer func() { close(gate); session.workers.Wait() }()
	request := ffiRequest{Version: FFIProtocol, Operation: "accept_result", Lease: 1, RequestID: 1, TimeoutNanos: int64(time.Millisecond)}
	response, err := decodeFFIResponse(session.handle(context.Background(), encodeFFIRequest(request)).Payload)
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != CodeDeadlineExceeded {
		t.Fatalf("decision timeout: %s", response.Code)
	}
}

func TestFFIBindingCloseRetainsUndecidedResultUntilCleanupEnds(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	routes := newRemoteRouteSet(1, normalizeLimits(Limits{}), nil, &remoteBinding{close: func() error { return nil }})
	session := &ffiSession{
		limits: normalizeLimits(Limits{}), leases: map[uint64]*RouteSet{1: routes},
		retiring: make(map[uint64]*ffiLeaseCleanup), shutdownDone: make(chan struct{}), cancelCalls: func() {},
		results: map[uint64]ffiPendingResult{1: {lease: 1, bytes: 7, result: &Result{accept: func(accept bool) error {
			if accept {
				t.Error("binding cleanup accepted an undecided result")
			}
			close(started)
			<-release
			return nil
		}}}}, resultBytes: 7,
	}
	t.Cleanup(func() {
		close(release)
		if err := session.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	cleanup := session.beginBindingClose(1, false)
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := session.decideResult(context.Background(), 1, 1, false); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	count, bytes := session.decidingResults, session.resultBytes
	session.mu.Unlock()
	if count != 1 || bytes != 7 {
		t.Fatalf("retiring result lost reservation: count=%d bytes=%d", count, bytes)
	}
	select {
	case <-cleanup.done:
		t.Fatal("binding cleanup completed while discard was blocked")
	default:
	}
}
