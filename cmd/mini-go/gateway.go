package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	rpcgateway "github.com/d7z-team/mini-go/rpc/gateway"
	rpcrouter "github.com/d7z-team/mini-go/rpc/router"
)

func runGateway(environment commandEnvironment, args []string, stdout, stderr io.Writer) (err error) {
	flags := flag.NewFlagSet("mini-go gateway", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listenAddress := "ws://127.0.0.1:7231/rpc"
	var tlsCertificate, tlsKey string
	flags.StringVar(&listenAddress, "listen", listenAddress, "Gateway address using ws://, wss://, or ws+unix://")
	flags.StringVar(&tlsCertificate, "tls-cert", "", "TLS certificate file required by a wss listener")
	flags.StringVar(&tlsKey, "tls-key", "", "TLS private key file required by a wss listener")
	shutdownTimeout := flags.Duration("shutdown-timeout", defaultShutdownTimeout, "total shutdown deadline")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("gateway does not accept positional arguments")
	}
	shutdown, finish, err := environment.shutdownPolicy(*shutdownTimeout)
	if err != nil {
		return err
	}
	defer finish()
	listenAddress = strings.TrimSpace(listenAddress)
	tlsCertificate = strings.TrimSpace(tlsCertificate)
	tlsKey = strings.TrimSpace(tlsKey)
	tlsCertificate = environment.path(tlsCertificate)
	tlsKey = environment.path(tlsKey)
	address, err := rpcgateway.ParseAddress(listenAddress)
	if err != nil {
		return err
	}
	if address.Path == "/publish" {
		return errors.New("Gateway consumer path /publish is reserved for provider sessions")
	}
	tlsConfig, err := gatewayTLSConfig(address, tlsCertificate, tlsKey)
	if err != nil {
		return err
	}

	gateway := rpcrouter.New(rpcrouter.Options{})
	defer func() { err = errors.Join(err, gateway.Shutdown(shutdown.begin())) }()
	catalog, err := rpcgateway.NewCatalogProvider(gateway)
	if err != nil {
		return err
	}
	if _, err := gateway.Register(catalog, rpcrouter.RegistrationOptions{Name: "minigo.gateway.catalog"}); err != nil {
		return err
	}
	publications, err := rpcgateway.NewPublicationRegistry(gateway, rpcgateway.PublicationRegistryOptions{})
	if err != nil {
		return err
	}
	server, err := rpcgateway.NewServer(gateway, rpcgateway.ServerOptions{
		TLSConfig: tlsConfig, Publications: publications,
		ShutdownTimeout: *shutdownTimeout,
		Drain: func(drainCtx context.Context) error {
			shutdown.begin()
			gateway.BeginShutdown()
			return nil
		},
	})
	if err != nil {
		return err
	}
	listener, err := rpcgateway.Listen(listenAddress)
	if err != nil {
		return err
	}
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "gateway listening on %s\n", listenAddress)
	}
	return rpcgateway.Serve(environment.context(), listener, server)
}

func gatewayTLSConfig(address rpcgateway.Address, certificatePath, keyPath string) (*tls.Config, error) {
	if address.Scheme == "wss" && (certificatePath == "" || keyPath == "") {
		return nil, errors.New("wss Gateway listener requires -tls-cert and -tls-key")
	}
	if address.Scheme != "wss" && (certificatePath != "" || keyPath != "") {
		return nil, errors.New("-tls-cert and -tls-key require a wss Gateway listener")
	}
	if address.Scheme != "wss" {
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load Gateway TLS certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}, nil
}
