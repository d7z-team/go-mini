package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/d7z-team/mini-go/rpc"
	service "github.com/d7z-team/mini-go/testdata/rpc/generated/go/service"
	"github.com/d7z-team/mini-go/tooling/rpccheck"
)

func run() error {
	if len(os.Args) != 3 {
		return errors.New("usage: mini-go-rpc-peer-go server|client address")
	}
	if os.Args[1] == "browser-server" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return rpccheck.RunBrowserPeer(ctx, os.Args[2], os.DirFS("."), os.Stdin, os.Stdout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if os.Args[1] == "gateway-server" || os.Args[1] == "gateway-client" {
		return rpccheck.RunGatewayPeer(ctx, os.Args[1], os.Args[2], os.Stdin, os.Stdout)
	}
	handler := &rpccheck.Laboratory{}
	provider, err := service.NewLaboratoryProvider(handler)
	if err != nil {
		return err
	}
	binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider)
	if err != nil {
		return err
	}
	var conn net.Conn
	switch os.Args[1] {
	case "server":
		listener, err := net.Listen("tcp", os.Args[2])
		if err != nil {
			return err
		}
		defer listener.Close()
		if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"address": listener.Addr().String()}); err != nil {
			return err
		}
		stop := context.AfterFunc(ctx, func() { listener.Close() })
		defer stop()
		conn, err = listener.Accept()
		if err != nil {
			return err
		}
	case "client":
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", os.Args[2])
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid peer mode %q", os.Args[1])
	}
	endpoint, err := rpc.OpenEndpoint(rpccheck.MessageConn{Conn: conn}, rpc.EndpointServices{Binder: binder}, rpc.EndpointOptions{Limits: rpc.Limits{MaxFrameBytes: 256}})
	if err != nil {
		conn.Close()
		return err
	}
	defer endpoint.Close()
	stop := context.AfterFunc(ctx, func() { endpoint.Close() })
	defer stop()
	report, err := rpccheck.Exercise(ctx, endpoint)
	if err != nil {
		return err
	}
	if err := rpccheck.ExerciseResourceRetry(ctx, endpoint); err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return err
	}
	if os.Args[1] == "server" {
		select {
		case <-endpoint.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	} else { // The runner releases the client after both directions finish.
		var line string
		if _, err := fmt.Fscanln(os.Stdin, &line); err != nil {
			return err
		}
	}
	if err := endpoint.Shutdown(ctx); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		return err
	}
	if handler.Released.Load() != 2 {
		return fmt.Errorf("resource close count %d", handler.Released.Load())
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
