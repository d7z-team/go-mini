package rpc

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBindingReportsExactContractSupport(t *testing.T) {
	method := Method{ID: "example::Files.Open", Service: "example::Files", Name: "Open", ContractHash: testContractHash}
	resource := Method{ID: "example::File.Read", Service: "example::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
	other := method
	other.ID, other.Name = "example::Files.Stat", "Stat"
	mismatch := method
	mismatch.ContractHash = strings.Repeat("b", 64)
	resourceMismatch := resource
	resourceMismatch.ResourceTypeHash = strings.Repeat("c", 64)
	var calls atomic.Int32
	provider, err := NewProvider(MethodBinding{Method: method, Invoke: func(context.Context, []Value) ([]Value, error) {
		calls.Add(1)
		return nil, nil
	}}, MethodBinding{Method: resource})
	if err != nil {
		t.Fatal(err)
	}
	local, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	remote, _ := openEndpointPair(t, nil, local, EndpointOptions{})
	for _, test := range []struct {
		name    string
		methods []Method
		code    Code
	}{
		{"supported", []Method{method, resource}, ""},
		{"missing method", []Method{other}, CodeUnimplemented},
		{"method contract", []Method{mismatch}, CodeFailedPrecondition},
		{"resource contract", []Method{method, resourceMismatch}, CodeFailedPrecondition},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, binder := range []Binder{local, remote} {
				routes, err := binder.Bind(context.Background(), testBindRequest(test.methods...))
				if routes != nil {
					defer routes.Close()
				}
				if codeOf(err) != test.code || (routes != nil) != (test.code == "") {
					t.Fatalf("%T: binding = %v, %v; want %s", binder, routes, err, test.code)
				}
			}
			lease, err := provider.BindRPC(context.Background(), testBindRequest(test.methods...))
			if lease != nil {
				defer lease.Close()
			}
			if codeOf(err) != test.code {
				t.Fatalf("provider binding: %v; want %s", err, test.code)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("binding invoked a business method")
	}
}

func TestEmptyHostReturnsUnimplemented(t *testing.T) {
	host, err := NewHost(HostOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, err := host.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	response := invokeFFI(context.Background(), t, session.(*ffiSession), ffiRequest{Operation: "open", Contract: testContract(method)})
	if response.Code != CodeUnimplemented || response.Lease != 0 {
		t.Fatalf("empty host binding = %#v", response)
	}
	state := session.(*ffiSession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.reservations != 0 || len(state.leases) != 0 {
		t.Fatal("failed binding retained session state")
	}
}
