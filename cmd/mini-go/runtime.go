package main

import (
	"context"
	"io"
	"os"
	"slices"

	"github.com/d7z-team/mini-go/rpc"
	runtimepkg "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/stdlib"
	stdlibhost "github.com/d7z-team/mini-go/stdlib/host"
)

func commandRuntimeOptions(directory string, stdin io.Reader, stdout, stderr io.Writer, program *runtimepkg.Program, remote rpc.Binder) (runtimepkg.InstanceOptions, func(context.Context) error, error) {
	capabilities := program.RequiredHostCapabilities()
	hints := stdlib.PackageCapabilities()
	for _, path := range program.PackagePaths() {
		capabilities = append(capabilities, hints[path]...)
	}
	slices.Sort(capabilities)
	capabilities = slices.Compact(capabilities)
	if len(capabilities) == 0 && remote == nil {
		return runtimepkg.InstanceOptions{}, func(context.Context) error { return nil }, nil
	}
	host, err := stdlibhost.NewDefault(stdlibhost.DefaultOptions{
		Capabilities: capabilities,
		Stdin:        stdin,
		Directory:    directory,
		Stdout:       stdout,
		Stderr:       stderr,
		Environ:      os.Environ(),
		Fallback:     remote,
	})
	if err != nil {
		return runtimepkg.InstanceOptions{}, nil, err
	}
	return runtimepkg.InstanceOptions{FFI: host}, host.Shutdown, nil
}
