package rpc

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkLocalBinder(b *testing.B) {
	bindings := make([]MethodBinding, 32)
	for i := range bindings {
		name := fmt.Sprintf("Method%d", i)
		bindings[i] = MethodBinding{
			Method: Method{ID: "bench::Service." + name, Service: "bench::Service", Name: name, ContractHash: testContractHash},
			Invoke: func(context.Context, []Value) ([]Value, error) { return nil, nil },
		}
	}
	resource := Method{ID: "bench::File.Read", Service: "bench::File", Name: "Read", ContractHash: testContractHash, ResourceTypeHash: testResourceHash}
	bindings = append(bindings, MethodBinding{Method: resource})
	provider, err := NewProvider(bindings...)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("construct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := NewLocalBinder(LocalBinderOptions{}, provider); err != nil {
				b.Fatal(err)
			}
		}
	})
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		b.Fatal(err)
	}
	for _, withResource := range []bool{false, true} {
		name := "service"
		request := testBindRequest(bindings[0].Method)
		if withResource {
			name = "resource"
			request.Contract.Methods = append(request.Contract.Methods, resource)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				routes, err := binder.Bind(context.Background(), request)
				if err != nil {
					b.Fatal(err)
				}
				if err := routes.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
