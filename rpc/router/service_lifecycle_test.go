package router

import (
	"fmt"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
)

func TestPublicationReplacementChargesRetiredOwners(t *testing.T) {
	router := New(Options{Limits: rpc.Limits{MaxBindings: 2}})
	t.Cleanup(func() { _ = router.ForceShutdown(t.Context()) })
	method := testMethod()
	first, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "first")}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := router.Bind(t.Context(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := router.Replace(first, []ProviderEntry{{Provider: testProvider(t, method, "second")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Replace(second, []ProviderEntry{{Provider: testProvider(t, method, "third")}}); err == nil {
		t.Fatal("retired binding did not retain owner capacity")
	} else if code, _ := rpc.CodeOf(err); code != rpc.CodeResourceExhausted {
		t.Fatal(err)
	}
	if got := callValue(t, router, method); got != "second" {
		t.Fatalf("failed replacement changed routing: %s", got)
	}
	if err := old.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Replace(second, []ProviderEntry{{Provider: testProvider(t, method, "third")}}); err != nil {
		t.Fatal(err)
	}
	if got := callValue(t, router, method); got != "third" {
		t.Fatal(got)
	}
}

func TestRoutingCountersFollowServiceLifetime(t *testing.T) {
	router := New(Options{})
	defer router.Shutdown(t.Context())
	for i := range 100 {
		method := testMethod()
		method.Service = fmt.Sprintf("example/service::Service%d", i)
		method.ID = method.Service + ".Hello"
		first, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "first")}})
		if err != nil {
			t.Fatal(err)
		}
		if got := callValue(t, router, method); got != "first" {
			t.Fatal(got)
		}
		counter := router.roundRobin[method.Service]
		second, err := router.Replace(first, []ProviderEntry{{Provider: testProvider(t, method, "second")}})
		if err != nil {
			t.Fatal(err)
		}
		if router.roundRobin[method.Service] != counter {
			t.Fatal("replacement reset routing sequence")
		}
		third, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "third")}})
		if err != nil {
			t.Fatal(err)
		}
		if err := second.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		if router.roundRobin[method.Service] != counter {
			t.Fatal("closing one registration lost service sequence")
		}
		if err := third.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(router.roundRobin) != 0 {
			t.Fatal("closed service retained routing state")
		}
	}
	method := testMethod()
	if _, err := router.Publish([]ProviderEntry{{Provider: testProvider(t, method, "last")}}); err != nil {
		t.Fatal(err)
	}
	callValue(t, router, method)
	if err := router.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(router.roundRobin) != 0 {
		t.Fatal("shutdown retained routing state")
	}
}
