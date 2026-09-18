package router

import (
	"context"
	"testing"

	"github.com/d7z-team/mini-go/rpc"
)

func FuzzRouterLifecycle(f *testing.F) {
	f.Add([]byte{0, 0, 1, 2, 3, 1, 4})
	f.Add([]byte{0, 1, 0, 1, 5, 2, 3})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			t.Skip()
		}
		method := testMethod()
		router := New(Options{})
		var registrations []*Registration
		var routes []*rpc.RouteSet
		for _, operation := range operations {
			switch operation % 6 {
			case 0:
				registration, err := router.Register(testProvider(t, method, "value"), RegistrationOptions{Priority: int(operation % 3), MaxLeases: 4})
				if err == nil {
					registrations = append(registrations, registration)
				}
			case 1:
				bound, err := router.Bind(context.Background(), rpc.BindRequest{Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: []rpc.Method{method}}})
				if err == nil {
					routes = append(routes, bound)
				}
			case 2:
				if len(routes) != 0 {
					result, err := routes[int(operation)%len(routes)].Call(context.Background(), rpc.Call{Method: method})
					if err == nil {
						_ = result.Discard(context.Background())
					}
				}
			case 3:
				if len(routes) != 0 {
					index := int(operation) % len(routes)
					_ = routes[index].Close()
					routes = append(routes[:index], routes[index+1:]...)
				}
			case 4:
				if len(registrations) != 0 {
					registrations[int(operation)%len(registrations)].SetHealthy(operation&1 == 0)
				}
			case 5:
				if len(registrations) != 0 {
					index := int(operation) % len(registrations)
					_ = registrations[index].ForceClose(context.Background())
					registrations = append(registrations[:index], registrations[index+1:]...)
				}
			}
		}
		if err := router.ForceShutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, routes := range routes {
			_ = routes.Close()
		}
		if status := router.Status(); status.State != Closed {
			t.Fatalf("Router status = %#v", status)
		}
	})
}
