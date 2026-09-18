package rpccheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/d7z-team/mini-go/rpc"
	"github.com/d7z-team/mini-go/rpc/gateway"
	"github.com/d7z-team/mini-go/rpc/router"
	service "github.com/d7z-team/mini-go/testdata/rpc/generated/go/service"
)

// GatewayReport records control-plane observations made over real WebSockets.
type GatewayReport struct {
	Publications int  `json:"publications"`
	Resolved     bool `json:"resolved"`
	Retained     bool `json:"retained"`
	Revoked      bool `json:"revoked"`
}

// RunGatewayPeer serves a Gateway or exercises its publication and discovery contracts.
func RunGatewayPeer(ctx context.Context, mode, address string, input io.Reader, output io.Writer) error {
	if mode == "gateway-server" {
		routes := router.New(router.Options{})
		defer func() { _ = routes.ForceShutdown(context.Background()) }()
		registry, err := gateway.NewPublicationRegistry(routes, gateway.PublicationRegistryOptions{DrainTimeout: time.Second})
		if err != nil {
			return err
		}
		catalog, err := gateway.NewCatalogProvider(routes)
		if err != nil {
			return err
		}
		if _, err := routes.Register(catalog, router.RegistrationOptions{}); err != nil {
			return err
		}
		server, err := gateway.NewServer(routes, gateway.ServerOptions{Publications: registry})
		if err != nil {
			return err
		}
		listener, err := gateway.Listen("ws://" + address + "/rpc")
		if err != nil {
			return err
		}
		defer listener.Close()
		running, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- gateway.Serve(running, listener, server) }()
		if err := json.NewEncoder(output).Encode(map[string]string{"address": "ws://" + listener.Addr().String()}); err != nil {
			return err
		}
		var command string
		if _, err := fmt.Fscanln(input, &command); err != nil {
			return err
		}
		cancel()
		if err := <-done; err != nil {
			return err
		}
		return server.Shutdown(ctx)
	}
	if mode != "gateway-client" {
		return fmt.Errorf("invalid Gateway mode %q", mode)
	}
	consumer, err := gateway.Dial(ctx, address+"/rpc", gateway.DialOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = consumer.Shutdown(context.Background()) }()
	catalog, err := gateway.NewClient(ctx, consumer)
	if err != nil {
		return err
	}
	defer catalog.Close()
	snapshot, err := catalog.Snapshot(ctx)
	if err != nil {
		return err
	}
	bundle, err := router.NewMRPCBundle("fixture/control", []router.MRPCFile{{Path: "control.mrpc", Text: "package control\n"}})
	if err != nil {
		return err
	}
	provider, err := service.NewLaboratoryProvider(&Laboratory{})
	if err != nil {
		return err
	}
	var connections []*rpc.Endpoint
	var pinned []*service.LaboratoryClient
	defer func() {
		for _, client := range pinned {
			client.Close()
		}
		for _, endpoint := range connections {
			_ = endpoint.Shutdown(context.Background())
		}
	}()
	for _, generation := range []uint64{1, 1, 2} {
		publisher, err := gateway.NewPublisher("peer-worker", generation, []gateway.PublicationProvider{{Provider: provider, Bundles: []router.MRPCBundle{bundle}}})
		if err != nil {
			return err
		}
		binder, err := rpc.NewLocalBinder(rpc.LocalBinderOptions{}, provider, publisher.Provider())
		if err != nil {
			return err
		}
		endpoint, err := gateway.Dial(ctx, address+"/publish", gateway.DialOptions{Services: rpc.EndpointServices{Binder: binder}})
		if err != nil {
			return err
		}
		connections = append(connections, endpoint)
		if err := publisher.WaitPublished(ctx); err != nil {
			return err
		}
		snapshot, err = catalog.Watch(ctx, snapshot)
		if err != nil {
			return err
		}
		if len(snapshot.References) != 1 {
			return fmt.Errorf("catalog references: %v", snapshot.References)
		}
		resolved, err := catalog.ResolveContract(ctx, snapshot.References[0])
		if err != nil {
			return err
		}
		if resolved.Hash != bundle.Hash {
			return errors.New("resolved another contract")
		}
		client, err := service.BindLaboratoryClient(ctx, consumer, rpc.BindOptions{})
		if err != nil {
			return err
		}
		pinned = append(pinned, client)
		for _, lease := range pinned {
			node, err := lease.Tree(ctx, service.Node{Value: 42})
			if err != nil || node.Value != 42 {
				return fmt.Errorf("retained lease: %v", err)
			}
		}
	}
	if err := ExerciseResourceRetry(ctx, consumer); err != nil {
		return err
	}
	for _, client := range pinned {
		if err := client.Close(); err != nil {
			return err
		}
	}
	pinned = nil
	for _, endpoint := range connections {
		if err := endpoint.Shutdown(ctx); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	for len(snapshot.References) != 0 {
		snapshot, err = catalog.Watch(ctx, snapshot)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(output).Encode(GatewayReport{Publications: 3, Resolved: true, Retained: true, Revoked: true})
}
