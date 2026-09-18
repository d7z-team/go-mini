package router

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/rpc"
)

func TestRouterBindingFailureReasons(t *testing.T) {
	for _, test := range []struct {
		name string
		code rpc.Code
	}{
		{"empty", rpc.CodeUnimplemented},
		{"matching", ""},
		{"contract", rpc.CodeFailedPrecondition},
		{"missing method", rpc.CodeUnimplemented},
		{"unhealthy", rpc.CodeUnavailable},
		{"draining", rpc.CodeUnavailable},
		{"labels", rpc.CodeUnavailable},
		{"capacity", rpc.CodeResourceExhausted},
		{"canceled", rpc.CodeCanceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := New(Options{})
			defer router.ForceShutdown(context.Background())
			method := testMethod()
			request := rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}
			if test.name != "empty" {
				declared := method
				if test.name == "contract" {
					declared.ContractHash = strings.Repeat("b", 64)
				} else if test.name == "missing method" {
					declared.Name, declared.ID = "Other", declared.Service+".Other"
				}
				registration, err := router.Register(testProvider(t, declared, "value"), RegistrationOptions{MaxLeases: 1})
				if err != nil {
					t.Fatal(err)
				}
				switch test.name {
				case "unhealthy":
					registration.SetHealthy(false)
				case "draining":
					if err := registration.Drain(context.Background()); err != nil {
						t.Fatal(err)
					}
				case "labels":
					request.Options.Labels = map[string]string{"zone": "missing"}
				case "capacity":
					routes, err := router.Bind(context.Background(), request)
					if err != nil {
						t.Fatal(err)
					}
					defer routes.Close()
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.name == "canceled" {
				cancel()
			}
			routes, err := router.Bind(ctx, request)
			if routes != nil {
				defer routes.Close()
			}
			if code, _ := rpc.CodeOf(err); code != test.code || (routes != nil) != (test.code == "") {
				t.Fatalf("binding = %v, %v; want %s", routes, err, test.code)
			}
			for _, status := range router.Status().Registrations {
				if status.PendingBinds != 0 {
					t.Fatal("binding retained reservation")
				}
			}
		})
	}
}

type failingBindingProvider struct {
	rpc.Provider
	code rpc.Code
}

type blockingBindProvider struct {
	rpc.Provider
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

type delayedCloseBindProvider struct {
	rpc.Provider
	bound        chan struct{}
	bindRelease  chan struct{}
	closeStarted chan struct{}
	closeRelease chan struct{}
	closeErr     error
	boundOnce    sync.Once
	bindOnce     sync.Once
	closeOnce    sync.Once
}

func (p *delayedCloseBindProvider) BindRPC(_ context.Context, request rpc.BindRequest) (rpc.ProviderLease, error) {
	p.boundOnce.Do(func() { close(p.bound) })
	<-p.bindRelease
	lease, err := p.Provider.BindRPC(context.Background(), request)
	if err != nil {
		return nil, err
	}
	return &delayedCloseProviderLease{
		ProviderLease: lease, started: p.closeStarted, release: p.closeRelease, err: p.closeErr,
	}, nil
}

func (p *delayedCloseBindProvider) releaseBind() {
	p.bindOnce.Do(func() { close(p.bindRelease) })
}

func (p *delayedCloseBindProvider) releaseClose() {
	p.closeOnce.Do(func() { close(p.closeRelease) })
}

type delayedCloseProviderLease struct {
	rpc.ProviderLease
	started chan struct{}
	release chan struct{}
	err     error
	once    sync.Once
}

type bindErrorWithLeaseProvider struct {
	rpc.Provider
	bindErr  error
	closeErr error
}

func (p bindErrorWithLeaseProvider) BindRPC(ctx context.Context, request rpc.BindRequest) (rpc.ProviderLease, error) {
	lease, err := p.Provider.BindRPC(ctx, request)
	if err != nil {
		return nil, err
	}
	return failingCloseLease{ProviderLease: lease, err: p.closeErr}, p.bindErr
}

func (l *delayedCloseProviderLease) Close() error {
	l.once.Do(func() {
		close(l.started)
		<-l.release
		l.err = errors.Join(l.ProviderLease.Close(), l.err)
	})
	return l.err
}

func (p *blockingBindProvider) BindRPC(_ context.Context, request rpc.BindRequest) (rpc.ProviderLease, error) {
	p.startOnce.Do(func() { close(p.started) })
	<-p.release
	// Deliberately use a fresh context: this provider models a host bind that
	// cannot observe cancellation until its external operation returns.
	return p.Provider.BindRPC(context.Background(), request)
}

func (p *blockingBindProvider) unblock() {
	p.releaseOnce.Do(func() { close(p.release) })
}

func TestRouterForceShutdownRetainsBlockedBindOwner(t *testing.T) {
	method := testMethod()
	base := testProvider(t, method, "blocked")
	provider := &blockingBindProvider{Provider: base, started: make(chan struct{}), release: make(chan struct{})}
	var closes atomic.Int32
	router := New(Options{})
	registration, err := router.RegisterOwned(provider, RegistrationOptions{}, func() error {
		closes.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	request := rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}
	bindDone := make(chan error, 1)
	go func() {
		_, bindErr := router.Bind(context.Background(), request)
		bindDone <- bindErr
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("blocking provider did not receive BindRPC")
	}
	t.Cleanup(func() {
		provider.unblock()
		_ = router.ForceShutdown(context.Background())
	})
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := router.ForceShutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ForceShutdown while BindRPC is blocked = %v", err)
	}
	if got := registration.Status().PendingBinds; got != 1 {
		t.Fatalf("pending bind owner after timed out shutdown = %d, want 1", got)
	}
	if got := closes.Load(); got != 0 {
		t.Fatalf("owned provider closed before BindRPC returned: %d", got)
	}
	provider.unblock()
	select {
	case bindErr := <-bindDone:
		if code, _ := rpc.CodeOf(bindErr); code != rpc.CodeUnavailable {
			t.Fatalf("blocked bind result = %v, code=%s", bindErr, code)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked BindRPC did not finish after release")
	}
	if err := router.ForceShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("owned provider close count = %d, want 1", got)
	}
	if got := registration.Status().PendingBinds; got != 0 {
		t.Fatalf("pending bind owner after completion = %d", got)
	}
}

func TestRouterLateBindCloseRetainsOwnerUntilLeaseClose(t *testing.T) {
	method := testMethod()
	base := testProvider(t, method, "late")
	provider := &delayedCloseBindProvider{
		Provider: base, bound: make(chan struct{}), bindRelease: make(chan struct{}),
		closeStarted: make(chan struct{}), closeRelease: make(chan struct{}),
	}
	router := New(Options{Limits: rpc.Limits{MaxBindings: 1}})
	registration, err := router.RegisterOwned(provider, RegistrationOptions{}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	request := rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}
	bindDone := make(chan error, 1)
	go func() {
		_, bindErr := router.Bind(context.Background(), request)
		bindDone <- bindErr
	}()
	<-provider.bound
	t.Cleanup(func() {
		provider.releaseBind()
		provider.releaseClose()
		_ = router.Shutdown(context.Background())
	})
	closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := registration.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closing registration while BindRPC is blocked = %v", err)
	}
	provider.releaseBind()
	<-provider.closeStarted
	if got := registration.Status().PendingBinds; got != 1 {
		t.Fatalf("pending bind while returned lease closes = %d, want 1", got)
	}
	if _, err := router.Register(base, RegistrationOptions{}); codeOfRouter(err) != rpc.CodeResourceExhausted {
		t.Fatalf("register while late lease closes = %v, want resource exhausted", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := router.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown while late lease closes = %v", err)
	}
	provider.releaseClose()
	select {
	case bindErr := <-bindDone:
		if code := codeOfRouter(bindErr); code != rpc.CodeUnavailable {
			t.Fatalf("late bind result = %v, code=%s", bindErr, code)
		}
	case <-time.After(time.Second):
		t.Fatal("late BindRPC did not finish after lease close")
	}
	if err := router.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := registration.Status().PendingBinds; got != 0 {
		t.Fatalf("pending bind after late lease close = %d", got)
	}
}

func codeOfRouter(err error) rpc.Code {
	code, _ := rpc.CodeOf(err)
	return code
}

func TestRouterProviderErrorLeaseCloseIsReported(t *testing.T) {
	method := testMethod()
	closeErr := errors.New("provider-error lease close failed")
	providerErr := errors.New("provider bind failed")
	router := New(Options{})
	registration, err := router.Register(bindErrorWithLeaseProvider{
		Provider: testProvider(t, method, "unused"), bindErr: providerErr, closeErr: closeErr,
	}, RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Shutdown(context.Background())
	request := rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}}
	if routes, err := router.Bind(context.Background(), request); routes != nil || codeOfRouter(err) != rpc.CodeInternal {
		t.Fatalf("provider error bind = %v, %v", routes, err)
	}
	if err := registration.Close(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("registration close after provider error = %v, want %v", err, closeErr)
	}
}

func TestRouterCanceledBindLeaseCloseIsReported(t *testing.T) {
	method := testMethod()
	closeErr := errors.New("canceled lease close failed")
	provider := &delayedCloseBindProvider{
		Provider: testProvider(t, method, "unused"), bound: make(chan struct{}), bindRelease: make(chan struct{}),
		closeStarted: make(chan struct{}), closeRelease: make(chan struct{}), closeErr: closeErr,
	}
	router := New(Options{})
	registration, err := router.Register(provider, RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		provider.releaseBind()
		provider.releaseClose()
		_ = router.Shutdown(context.Background())
	}()
	ctx, cancel := context.WithCancel(context.Background())
	bindDone := make(chan error, 1)
	go func() {
		_, bindErr := router.Bind(ctx, rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
		bindDone <- bindErr
	}()
	<-provider.bound
	cancel()
	provider.releaseBind()
	<-provider.closeStarted
	provider.releaseClose()
	if err := <-bindDone; codeOfRouter(err) != rpc.CodeCanceled {
		t.Fatalf("canceled bind = %v", err)
	}
	if err := registration.Close(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("registration close after canceled bind = %v, want %v", err, closeErr)
	}
}

func TestRouterRejectedCommitLeaseCloseIsReported(t *testing.T) {
	method := testMethod()
	closeErr := errors.New("rejected lease close failed")
	provider := &delayedCloseBindProvider{
		Provider: testProvider(t, method, "unused"), bound: make(chan struct{}), bindRelease: make(chan struct{}),
		closeStarted: make(chan struct{}), closeRelease: make(chan struct{}), closeErr: closeErr,
	}
	router := New(Options{})
	registration, err := router.Register(provider, RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		provider.releaseBind()
		provider.releaseClose()
		_ = router.Shutdown(context.Background())
	}()
	bindDone := make(chan error, 1)
	go func() {
		_, bindErr := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
		bindDone <- bindErr
	}()
	<-provider.bound
	closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := registration.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closing registration during bind = %v", err)
	}
	cancel()
	provider.releaseBind()
	<-provider.closeStarted
	provider.releaseClose()
	if err := <-bindDone; codeOfRouter(err) != rpc.CodeUnavailable {
		t.Fatalf("rejected commit bind = %v", err)
	}
	if err := registration.Close(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("registration close after rejected commit = %v, want %v", err, closeErr)
	}
}

func TestRouterSelectsCompleteResourceContract(t *testing.T) {
	router := New(Options{})
	defer router.ForceShutdown(context.Background())
	method := testMethod()
	resource := rpc.Method{ID: "example/service::Reader.Read", Service: "example/service::Reader", Name: "Read", ContractHash: contractHash, ResourceTypeHash: contractHash}
	if _, err := router.Register(testProvider(t, method, "incomplete"), RegistrationOptions{}); err != nil {
		t.Fatal(err)
	}
	complete, err := rpc.NewProvider(
		rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
			return []rpc.Value{{Type: "string", Data: "complete"}}, nil
		}},
		rpc.MethodBinding{Method: resource},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Register(complete, RegistrationOptions{}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		routes, err := router.Bind(t.Context(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method, resource}}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := routes.Call(t.Context(), rpc.Call{Method: method})
		if err != nil {
			_ = routes.Abort()
			t.Fatal(err)
		}
		value := result.Values[0].Data
		_ = result.Discard(context.Background())
		_ = routes.Close()
		if value != "complete" {
			t.Fatalf("selected incomplete provider: %v", value)
		}
	}
}

func (p failingBindingProvider) BindRPC(context.Context, rpc.BindRequest) (rpc.ProviderLease, error) {
	return nil, rpc.StatusError{Code: p.code, Message: "provider rejected binding"}
}

func FuzzRouterBindingErrors(f *testing.F) {
	f.Add(byte(0), byte(0))
	f.Add(byte(1), byte(1))
	f.Add(byte(2), byte(0))
	f.Add(byte(5), byte(1))
	f.Fuzz(func(t *testing.T, failure, order byte) {
		codes := []rpc.Code{rpc.CodePermissionDenied, rpc.CodeUnavailable, rpc.CodeDeadlineExceeded, rpc.CodeResourceExhausted, rpc.CodeFailedPrecondition, rpc.CodeUnavailable}
		want := codes[int(failure)%len(codes)]
		unhealthy := int(failure)%len(codes) == len(codes)-1
		router := New(Options{})
		defer router.ForceShutdown(context.Background())
		method := testMethod()
		base := testProvider(t, method, "unused")
		providers := []rpc.Provider{failingBindingProvider{base, rpc.CodeUnimplemented}, failingBindingProvider{base, want}}
		if order%2 == 1 {
			providers[0], providers[1] = providers[1], providers[0]
		}
		for _, provider := range providers {
			registration, err := router.Register(provider, RegistrationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if unhealthy && provider.(failingBindingProvider).code == rpc.CodeUnavailable {
				registration.SetHealthy(false)
			}
		}
		for range 3 {
			routes, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
			if code, _ := rpc.CodeOf(err); routes != nil || code != want {
				t.Fatalf("binding = %v, %v; want %s", routes, err, want)
			}
			for _, status := range router.Status().Registrations {
				if status.PendingBinds != 0 || status.ActiveLeases != 0 {
					t.Fatalf("failed binding retained state: %#v", status)
				}
			}
		}
	})
}
