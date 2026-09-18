package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayUsesCommandContext(t *testing.T) {
	for _, when := range []string{"before startup", "after listen"} {
		t.Run(when, func(t *testing.T) {
			directory := t.TempDir()
			socket := filepath.Join(directory, "gateway.sock")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if when == "before startup" {
				cancel()
			}
			output := &cancelOnWrite{cancel: cancel}
			if err := runCLIContext(ctx, []string{"-C", directory, "gateway", "-listen", "ws+unix://" + socket}, output, io.Discard); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "gateway listening on") {
				t.Fatalf("missing startup output: %q", output.String())
			}
			if _, err := os.Stat(socket); !os.IsNotExist(err) {
				t.Fatalf("socket remains after shutdown: %v", err)
			}
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatalf("listener not released: %v", err)
			}
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGatewayReportsListenFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var output bytes.Buffer
	err = runGateway(testCommandEnvironment(t, t.TempDir()), []string{"-listen", "ws://" + listener.Addr().String() + "/rpc"}, &output, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "listen on Gateway") {
		t.Fatalf("listen failure = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("failed listener announced startup: %q", output.String())
	}
}

type cancelOnWrite struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnWrite) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.cancel()
	return n, err
}
