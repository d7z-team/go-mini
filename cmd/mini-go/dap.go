package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/d7z-team/mini-go/compiler"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/tooling/dap"
)

func runDAPContext(ctx context.Context, environment commandEnvironment, input io.Reader, output io.Writer) error {
	session := dap.NewSession(input, output, func(ctx context.Context, config dap.LaunchConfig) (dap.LaunchTarget, error) {
		return launchDAPTarget(ctx, environment, config)
	})
	return session.Serve(ctx)
}

func launchDAPTarget(ctx context.Context, environment commandEnvironment, config dap.LaunchConfig) (dap.LaunchTarget, error) {
	directory := environment.path(config.Directory)
	if config.Directory == "" {
		directory = environment.workingDir
	}
	launchEnvironment := environment
	launchEnvironment.workingDir = directory
	launchEnvironment.ctx = ctx
	operands := []string(nil)
	if config.Root != "" {
		operands = []string{config.Root}
	}
	sources := sourceOptions{module: config.Module}
	for _, mapping := range config.Sources {
		sources.mappings = append(sources.mappings, mapping.Module+"="+mapping.Directory)
	}
	build, err := loadBuildContext(launchEnvironment, buildOptions{
		sources: sources, tags: stringList(config.Tags), rpcTimeout: 10 * time.Second,
		optimization: compiler.OptimizationNone, symbols: true,
	}, operands)
	if err != nil {
		return dap.LaunchTarget{}, err
	}
	if len(build.roots) != 1 {
		return dap.LaunchTarget{}, errors.New("DAP launch requires exactly one package")
	}
	compilerSession, err := build.openCompiler()
	if err != nil {
		return dap.LaunchTarget{}, err
	}
	root := build.roots[0]
	prepared, err := compilerSession.PrepareContext(ctx, root, nil)
	if err != nil {
		return dap.LaunchTarget{}, err
	}
	if !prepared.Checked.OK() || prepared.Image == nil {
		if len(prepared.Checked.Diagnostics) == 0 {
			return dap.LaunchTarget{}, errors.New("compilation failed")
		}
		diagnostic := prepared.Checked.Diagnostics[0]
		return dap.LaunchTarget{}, fmt.Errorf("%s: %s", diagnostic.Code, diagnostic.Message)
	}
	program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		return dap.LaunchTarget{}, err
	}
	if prepared.Symbols == nil {
		return dap.LaunchTarget{}, errors.New("compiler did not produce debug symbols")
	}
	program, err = program.WithSymbols(*prepared.Symbols)
	if err != nil {
		return dap.LaunchTarget{}, err
	}
	output := dap.NewOutputBuffer()
	options, cleanup, err := commandRuntimeOptions(directory, nil, output.StdoutWriter(), output.StderrWriter(), program, nil)
	if err != nil {
		return dap.LaunchTarget{}, err
	}
	modulePath := root
	if build.module != nil && !build.explicit {
		modulePath = build.module.ModulePath
	}
	return dap.LaunchTarget{Locations: build.locations, Program: program, Options: options, Output: output, RootPath: build.sourceRoot, ModulePath: modulePath, Cleanup: func() error { return cleanup(context.Background()) }}, nil
}
