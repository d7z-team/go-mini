package rpccheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/d7z-team/mini-go/rpc"
)

func TestBrowserPeerStopClosesUpgradedConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input, command := io.Pipe()
	output, announcements := io.Pipe()
	t.Cleanup(func() {
		_ = input.Close()
		_ = command.Close()
		_ = output.Close()
		_ = announcements.Close()
	})
	done := make(chan error, 1)
	go func() { done <- RunBrowserPeer(ctx, "127.0.0.1:0", fstest.MapFS{}, input, announcements) }()
	var ready struct{ Address string }
	if err := json.NewDecoder(output).Decode(&ready); err != nil {
		t.Fatal(err)
	}
	var connections []*websocket.Conn
	for range 2 {
		conn, _, err := websocket.Dial(ctx, strings.Replace(ready.Address, "http", "ws", 1)+"/rpc", &websocket.DialOptions{Subprotocols: []string{rpc.EndpointProtocol}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		connections = append(connections, conn)
	}
	if _, err := fmt.Fprintln(command, "stop"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	for _, conn := range connections {
		for {
			_, _, err := conn.Read(deadline)
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("peer returned with an upgraded connection still open")
			}
			if err != nil {
				break
			}
		}
	}
}
