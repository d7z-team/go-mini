package main

import (
	"errors"
	"io"

	"github.com/d7z-team/mini-go/rpc"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func runProgram(environment commandEnvironment, args []string, stdout, stderr io.Writer) (err error) {
	ctx := environment.context()
	options, operands, err := parseBuildOptions("mini-go run", args, stderr, true, true)
	if err != nil {
		return err
	}
	shutdown, finish, err := environment.shutdownPolicy(options.shutdownTimeout)
	if err != nil {
		return err
	}
	defer finish()
	build, err := loadBuildContext(environment, options, operands)
	if err != nil {
		return err
	}
	if !build.explicit && len(operands) > 1 {
		return errors.New("run accepts one package")
	}
	if len(build.roots) != 1 {
		return errors.New("run package pattern must resolve to exactly one package")
	}
	root := build.roots[0]
	session, err := build.openCompiler()
	if err != nil {
		return err
	}
	endpoint, err := build.openEndpoint(ctx)
	if err != nil {
		return err
	}
	var remote rpc.Binder
	if endpoint != nil {
		remote = endpoint
		defer func() { err = errors.Join(err, endpoint.Shutdown(shutdown.begin())) }()
	}
	prepared, err := session.PrepareContext(ctx, root, nil)
	if err != nil {
		return err
	}
	if !prepared.Checked.OK() || prepared.Image == nil {
		writeDiagnostics(stderr, prepared.Checked.Diagnostics)
		return errors.New("compilation failed")
	}
	program, err := minigoruntime.LoadExecutionImage(*prepared.Image)
	if err != nil {
		return err
	}
	if prepared.Symbols != nil {
		program, err = program.WithSymbols(*prepared.Symbols)
		if err != nil {
			return err
		}
	}
	runtimeOptions, cleanup, err := commandRuntimeOptions(environment.workingDir, environment.stdin, stdout, stderr, program, remote)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cleanup(shutdown.begin())) }()
	runtimeOptions.Limits.MaxSteps = options.maxSteps
	instance, err := program.Instantiate(ctx, runtimeOptions)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, instance.Shutdown(shutdown.begin())) }()
	_, err = instance.CallMain(ctx)
	return err
}
