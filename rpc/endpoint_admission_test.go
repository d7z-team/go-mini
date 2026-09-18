package rpc

import (
	"context"
	"testing"
	"time"
)

func TestEndpointDispatchWritesAdmissionBeforeOutcome(t *testing.T) {
	method := Method{ID: "example::Service.Value", Service: "example::Service", Name: "Value", ContractHash: testContractHash}
	provider := newTestProvider(t, method, func(context.Context, []Value) ([]Value, error) { return nil, nil })
	binder, err := NewLocalBinder(LocalBinderOptions{}, provider)
	if err != nil {
		t.Fatal(err)
	}
	conn, peer := newTestMessagePipe()
	endpoint, err := OpenEndpoint(conn, EndpointServices{Binder: binder}, EndpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = endpoint.Shutdown(context.Background())
		_ = peer.Close()
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// Read the local HELLO; this unit drives admitted dispatch directly.
	hello, err := peer.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := decodeEndpointFragment(hello, endpoint.limits)
	if err != nil {
		t.Fatal(err)
	}
	contract := testContract(method)
	arguments, err := EncodeValues(nil, endpoint.limits)
	if err != nil {
		t.Fatal(err)
	}
	requests := []endpointFrame{
		{Kind: "bind", ID: 1, Contract: &contract},
		{Kind: "call", ID: 2, Binding: 1, Call: &wireCall{Method: method, Arguments: arguments}},
	}
	var assembly endpointAssembly
	previous := fragment.messageID
	for _, request := range requests {
		work, stop := context.WithCancel(ctx)
		endpoint.mu.Lock()
		control := request.Kind == "bind"
		endpoint.active[request.ID] = endpointActive{cancel: stop, control: control, binding: request.Binding}
		if control {
			endpoint.activeControls++
			endpoint.inboundReservations++
		} else {
			endpoint.activeData++
			endpoint.resultReservations++
		}
		endpoint.inboundOperations[request.ID] = time.Now().Add(time.Minute)
		endpoint.dispatchWG.Add(1)
		endpoint.mu.Unlock()
		go endpoint.dispatch(work, request, stop)
		outcome := "done"
		if request.Kind == "call" {
			outcome = "offer"
		}
		for _, kind := range []string{"accepted", outcome} {
			var message []byte
			for message == nil {
				data, err := peer.Read(ctx)
				if err != nil {
					t.Fatal(err)
				}
				fragment, err := decodeEndpointFragment(data, endpoint.limits)
				if err != nil {
					t.Fatal(err)
				}
				message, err = assembly.append(fragment, &previous)
				if err != nil {
					t.Fatal(err)
				}
			}
			frame, err := decodeEndpointFrame(message, endpoint.limits)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Kind != kind || frame.TargetID != request.ID || frame.Code != "" {
				t.Fatalf("dispatch reply = %#v, want %s for operation %d", frame, kind, request.ID)
			}
		}
	}
}
