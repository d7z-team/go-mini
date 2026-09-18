package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type retryCloseResource struct {
	Resource
	attempts       atomic.Int32
	failures       int32
	panicOnFailure bool
}

type gatedCloseResource struct {
	Resource
	started, finish chan struct{}
	calls           atomic.Int32
}

func (r *gatedCloseResource) Close(ctx context.Context) error {
	if r.calls.Add(1) == 1 {
		close(r.started)
	}
	<-r.finish
	return ctx.Err()
}

func TestResourceCloseWaitCancellationKeepsSingleOwner(t *testing.T) {
	resource := &gatedCloseResource{started: make(chan struct{}), finish: make(chan struct{})}
	routes := newRouteSet(1, normalizeLimits(Limits{}), nil, nil)
	t.Cleanup(func() {
		select {
		case <-resource.finish:
		default:
			close(resource.finish)
		}
		if err := routes.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	exporter := &routeExporter{routes: routes, callID: 1}
	value, err := exporter.Export(resource, testResourceHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := routes.finishResult(1, []uint64{value.Resource.ObjectID}, true); err != nil {
		t.Fatal(err)
	}
	handle, err := routes.BindResource(*value.Resource)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { first <- handle.Close(ctx) }()
	<-resource.started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	second := make(chan error, 1)
	go func() { second <- handle.Close(context.Background()) }()
	if _, err := handle.Reference(context.Background(), routes); err == nil {
		t.Fatal("closing resource remained usable")
	}
	select {
	case err := <-second:
		t.Fatalf("close returned before cleanup: %v", err)
	default:
	}
	close(resource.finish)
	select {
	case err := <-second:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not finish")
	}
	if err := routes.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resource.calls.Load() != 1 {
		t.Fatalf("concurrent close invoked backend %d times", resource.calls.Load())
	}
}

func (r *retryCloseResource) Close(context.Context) error {
	if r.attempts.Add(1) <= r.failures {
		if r.panicOnFailure {
			panic("injected close failure")
		}
		return context.DeadlineExceeded
	}
	return nil
}

func TestResourceFailedCloseRemainsOwned(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/scenarios/close.json")
	if err != nil {
		t.Fatal(err)
	}
	type scenario struct {
		Name           string
		Failures       int32
		Retry          bool
		TerminalError  bool `json:"terminal_error"`
		Attempts       int32
		PanicOnFailure bool
	}
	var scenarios []scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	scenarios = append(scenarios, scenario{Name: "panic-before-retry", Failures: 1, Retry: true, Attempts: 2, PanicOnFailure: true})
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			method := Method{ID: "audit::Service.Open", Service: "audit::Service", Name: "Open", ContractHash: testContractHash}
			resource := &retryCloseResource{failures: scenario.Failures, panicOnFailure: scenario.PanicOnFailure}
			provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
				value, err := Export(ctx, resource, testResourceHash)
				return []Value{value}, err
			})
			binder, err := NewLocalBinder(LocalBinderOptions{Limits: Limits{MaxResources: 1}}, provider)
			if err != nil {
				t.Fatal(err)
			}
			routes, err := binder.Bind(context.Background(), testBindRequest(method))
			if err != nil {
				t.Fatal(err)
			}
			result, err := routes.Call(context.Background(), Call{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			if err := result.Accept(context.Background()); err != nil {
				t.Fatal(err)
			}
			handle, err := routes.BindResource(*result.Values[0].Resource)
			if err != nil {
				t.Fatal(err)
			}
			closeErr := handle.Close(context.Background())
			if scenario.PanicOnFailure {
				if code, _ := CodeOf(closeErr); code != CodeInternal {
					t.Fatal(closeErr)
				}
			} else if !errors.Is(closeErr, context.DeadlineExceeded) {
				t.Fatalf("first close: %v", closeErr)
			}
			if _, err := handle.Reference(context.Background(), routes); err == nil {
				t.Fatal("failed close allowed further business use")
			}
			if _, err := routes.Call(context.Background(), Call{Method: method}); err == nil {
				t.Fatal("failed close released its resource quota")
			} else if code, _ := CodeOf(err); code != CodeResourceExhausted {
				t.Fatal(err)
			}
			if scenario.Retry {
				if err := handle.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if err := routes.Shutdown(context.Background()); (err != nil) != scenario.TerminalError {
				t.Fatal(err)
			}
			if err := handle.Close(context.Background()); (err != nil) != scenario.TerminalError {
				t.Fatalf("terminal close: %v", err)
			}
			if got := resource.attempts.Load(); got != scenario.Attempts {
				t.Fatalf("failed Close lost resource ownership: close attempts=%d", got)
			}
		})
	}
}
