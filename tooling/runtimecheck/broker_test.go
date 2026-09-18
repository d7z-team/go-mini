package runtimecheck

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/d7z-team/mini-go/ffi"
)

func TestBrokerPreservesBinaryPayloadAndDiscardOwnership(t *testing.T) {
	var discarded, canceled atomic.Int64
	var late ffi.Completion
	bridge := ffi.CallFunc(func(_ context.Context, request ffi.Request, complete ffi.Completion) (ffi.Call, error) {
		if request.Route == "late" {
			late = complete
		} else {
			complete(ffi.Result{Payload: request.Payload, Discard: func() { discarded.Add(1) }})
		}
		return ffi.CancelFunc(func() { canceled.Add(1) }), nil
	})
	client := startBroker(t, bridge)
	if client.receive(t, "ready").Version != 1 {
		t.Fatal("broker version mismatch")
	}
	for id, operation := range []string{"consumed", "discard"} {
		client.send(t, brokerCommand{Operation: "start", ID: uint64(id + 1), Route: "echo", Payload: []byte{0, 255, 128}})
		reply := client.receive(t, "result")
		if string(reply.Payload) != string([]byte{0, 255, 128}) {
			t.Fatal("binary payload changed")
		}
		client.receive(t, "started")
		client.send(t, brokerCommand{Operation: operation, ID: uint64(id + 1)})
		client.send(t, brokerCommand{Operation: operation, ID: uint64(id + 1)})
	}
	client.send(t, brokerCommand{Operation: "start", ID: 3, Route: "late"})
	client.receive(t, "started")
	client.send(t, brokerCommand{Operation: "cancel", ID: 3})
	client.send(t, brokerCommand{Operation: "shutdown"})
	client.receive(t, "closed")
	if err := <-client.done; err != nil {
		t.Fatal(err)
	}
	late(ffi.Result{Payload: []byte("late"), Discard: func() { discarded.Add(1) }})
	if discarded.Load() != 2 || canceled.Load() != 1 {
		t.Fatalf("discarded=%d canceled=%d", discarded.Load(), canceled.Load())
	}
}

func TestBrokerReportsUnavailableRoutesFromActualProviders(t *testing.T) {
	host, err := newStandardHost()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client := startBroker(t, host)
	client.receive(t, "ready")
	client.send(t, brokerCommand{Operation: "start", ID: 1, Route: "unavailable", Payload: []byte{255}})
	event := client.receive(t, "start_error")
	if event.Code != "route_unavailable" || event.Error == "" {
		t.Fatalf("route error = %#v", event)
	}
	client.send(t, brokerCommand{Operation: "shutdown"})
	client.receive(t, "closed")
	if err := <-client.done; err != nil {
		t.Fatal(err)
	}
}
