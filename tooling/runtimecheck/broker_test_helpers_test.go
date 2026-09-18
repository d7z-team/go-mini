package runtimecheck

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/ffi"
)

type brokerClient struct {
	encoder *json.Encoder
	decoder *json.Decoder
	done    <-chan error
}

func startBroker(t *testing.T, bridge ffi.Bridge) brokerClient {
	t.Helper()
	client, server := net.Pipe()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() { defer close(exited); done <- serveHostBroker(ctx, bridge, server, server); server.Close() }()
	t.Cleanup(func() {
		cancel()
		client.Close()
		server.Close()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("broker did not shut down")
		}
	})
	return brokerClient{json.NewEncoder(client), json.NewDecoder(client), done}
}

func (client brokerClient) send(t *testing.T, command brokerCommand) {
	t.Helper()
	if err := client.encoder.Encode(command); err != nil {
		t.Fatal(err)
	}
}

func (client brokerClient) receive(t *testing.T, kind string) brokerEvent {
	t.Helper()
	var event brokerEvent
	if err := client.decoder.Decode(&event); err != nil {
		t.Fatal(err)
	}
	if event.Event != kind {
		t.Fatalf("event = %#v, want %s", event, kind)
	}
	return event
}
