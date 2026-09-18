package router

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/rpc"
)

func TestForceShutdownWaitCancellationRetainsRunningHandler(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(finish) })
	method := testMethod()
	provider, err := rpc.NewProvider(rpc.MethodBinding{Method: method, Invoke: func(context.Context, []rpc.Value) ([]rpc.Value, error) {
		close(started)
		<-finish
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	router := New(Options{})
	registration, err := router.Register(provider, RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := router.Bind(t.Context(), rpc.BindRequest{Contract: provider.RPCContract()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release(); _ = router.ForceShutdown(context.Background()) })
	callDone := make(chan struct{})
	go func() {
		result, _ := routes.Call(t.Context(), rpc.Call{Method: method})
		if result != nil {
			_ = result.Discard(context.Background())
		}
		close(callDone)
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- router.ForceShutdown(ctx) }()
	select {
	case err := <-shutdown:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ForceShutdown ignored wait cancellation")
	}
	if registration.Status().ActiveLeases != 1 {
		t.Fatal("running handler lost its lease owner")
	}
	release()
	<-callDone
	if err := router.ForceShutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if registration.Status().ActiveLeases != 0 {
		t.Fatal("finished handler retained lease")
	}
}
