package rpc

import (
	"context"
	"strconv"
	"testing"
)

func TestGuestResultExportFailureReleasesAll(t *testing.T) {
	for available := 0; available < 3; available++ {
		t.Run(strconv.Itoa(available), func(t *testing.T) {
			method := Method{ID: "audit::Service.Open", Service: "audit::Service", Name: "Open", ContractHash: testContractHash}
			guest := &ffiProvider{events: make(chan ffiProviderEvent, 8), done: make(chan struct{})}
			provider := newTestProvider(t, method, func(ctx context.Context, _ []Value) ([]Value, error) {
				result, err := guest.providerResult(ctx, ffiProviderOutcome{values: []Value{
					{Type: testResourceHash, Resource: &ResourceRef{Epoch: 1, ObjectID: 1, TypeHash: testResourceHash}},
					{Type: testResourceHash, Resource: &ResourceRef{Epoch: 1, ObjectID: 11, TypeHash: testResourceHash}},
					{Type: testResourceHash, Resource: &ResourceRef{Epoch: 1, ObjectID: 11, TypeHash: testResourceHash}},
					{Type: testResourceHash, Resource: &ResourceRef{Epoch: 1, ObjectID: 12, TypeHash: testResourceHash}},
					{Type: testResourceHash, Resource: &ResourceRef{Epoch: 1, ObjectID: 13, TypeHash: testResourceHash}},
				}})
				if err != nil {
					return nil, err
				}
				return result.Values, nil
			})
			binder, err := NewLocalBinder(LocalBinderOptions{Limits: Limits{MaxResources: 3}}, provider)
			if err != nil {
				t.Fatal(err)
			}
			routes, err := binder.Bind(context.Background(), testBindRequest(method))
			if err != nil {
				t.Fatal(err)
			}
			defer routes.Shutdown(context.Background())
			exporter := &routeExporter{routes: routes, callID: 0}
			var existing []uint64
			for id := 1; id <= 3-available; id++ {
				value, err := exporter.Export(&guestProviderResource{provider: guest, ref: ResourceRef{Epoch: 1, ObjectID: uint64(id), TypeHash: testResourceHash}}, testResourceHash)
				if err != nil {
					t.Fatal(err)
				}
				existing = append(existing, value.Resource.ObjectID)
			}
			if err := routes.finishResult(0, existing, true); err != nil {
				t.Fatal(err)
			}
			_, err = routes.Call(context.Background(), Call{Method: method})
			if codeOf(err) != CodeResourceExhausted {
				t.Fatalf("call error: %v", err)
			}
			released := make(map[uint64]int)
			for len(guest.events) != 0 {
				released[(<-guest.events).receiver.ObjectID]++
			}
			if len(released) != 3 || released[11] != 1 || released[12] != 1 || released[13] != 1 {
				t.Fatalf("new resources were not released exactly once, released=%v", released)
			}
		})
	}
}
