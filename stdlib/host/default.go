package host

import (
	"fmt"
	"io"
	"os"

	"github.com/d7z-team/mini-go/rpc"
	"github.com/d7z-team/mini-go/stdlib"
	"github.com/d7z-team/mini-go/stdlib/host/console"
	oshost "github.com/d7z-team/mini-go/stdlib/host/os"
)

// DefaultOptions configures the official native host providers.
type DefaultOptions struct {
	Capabilities []string
	Directory    string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Environ      []string
	Fallback     rpc.Binder
}

// NewDefault creates the official Go host providers selected by Capabilities.
func NewDefault(options DefaultOptions) (host *Host, err error) {
	providers := make([]Provider, 0, len(options.Capabilities))
	defer func() {
		if err == nil {
			return
		}
		for index := len(providers) - 1; index >= 0; index-- {
			if providers[index].Close != nil {
				_ = providers[index].Close()
			}
		}
	}()
	for _, capability := range options.Capabilities {
		switch stdlib.HostCapability(capability) {
		case stdlib.CapabilityConsole:
			provider, err := console.NewFmtConsoleProvider(&console.Streams{Stdin: options.Stdin, Stdout: options.Stdout, Stderr: options.Stderr})
			if err != nil {
				return nil, err
			}
			providers = append(providers, Provider{Capability: capability, RPC: provider})
		case stdlib.CapabilityFilesystem:
			backend, err := oshost.NewRootedFilesystem(options.Directory)
			if err != nil {
				return nil, err
			}
			provider, err := oshost.NewFilesystemProvider(backend)
			if err != nil {
				_ = backend.Close()
				return nil, err
			}
			providers = append(providers, Provider{Capability: capability, RPC: provider, Close: backend.Close})
		case stdlib.CapabilityEnvironment:
			entries := options.Environ
			if entries == nil {
				entries = os.Environ()
			}
			provider, err := oshost.NewEnvironmentProvider(oshost.Snapshot(entries))
			if err != nil {
				return nil, err
			}
			providers = append(providers, Provider{Capability: capability, RPC: provider})
		default:
			return nil, fmt.Errorf("standard-library capability %q has no default provider", capability)
		}
	}
	host, err = New(Options{Providers: providers, Fallback: options.Fallback})
	if err != nil {
		providers = nil // New already rolled back transferred cleanup functions.
	}
	return host, err
}
