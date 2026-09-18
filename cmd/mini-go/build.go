package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/rpc"
	rpcgateway "github.com/d7z-team/mini-go/rpc/gateway"
	"github.com/d7z-team/mini-go/stdlib"
)

type buildOptions struct {
	maxSteps        int64
	shutdownTimeout time.Duration
	sources         sourceOptions
	cacheLog        io.Writer
	rpcAddress      string
	rpcTimeout      time.Duration
	tags            stringList
	optimization    compiler.OptimizationLevel
	symbols         bool
}

func parseBuildOptions(name string, args []string, stderr io.Writer, withRPC, withCodegen bool) (buildOptions, []string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := buildOptions{cacheLog: stderr, rpcTimeout: 10 * time.Second}
	options.sources.flags(flags)
	if name == "mini-go run" {
		flags.Int64Var(&options.maxSteps, "max-steps", 0, "guest instruction budget: 0 default, -1 unlimited")
		flags.DurationVar(&options.shutdownTimeout, "shutdown-timeout", defaultShutdownTimeout, "total shutdown deadline")
	}
	flags.Var(&options.tags, "tag", "enable a Mini-Go build tag; repeatable")
	optimization := int(compiler.OptimizationDefault)
	if withCodegen {
		flags.IntVar(&optimization, "O", optimization, "compiler optimization level: 0, 1, or 2")
		flags.BoolVar(&options.symbols, "symbols", false, "generate detached program symbols")
	}
	if withRPC {
		addRPCFlags(flags, &options)
	}
	if err := flags.Parse(args); err != nil {
		return buildOptions{}, nil, err
	}
	if options.maxSteps < -1 {
		return buildOptions{}, nil, errors.New("-max-steps must be -1 or nonnegative")
	}
	if err := normalizeBuildOptions(&options); err != nil {
		return buildOptions{}, nil, err
	}
	if withCodegen {
		level, err := compiler.ParseOptimizationLevel(optimization)
		if err != nil {
			return buildOptions{}, nil, err
		}
		options.optimization = level
	}
	return options, flags.Args(), nil
}

func addRPCFlags(flags *flag.FlagSet, options *buildOptions) {
	flags.StringVar(&options.rpcAddress, "rpc", "", "remote Gateway address for MRPC contracts and providers")
	flags.DurationVar(&options.rpcTimeout, "rpc-timeout", options.rpcTimeout, "timeout for remote Gateway discovery")
}

func normalizeBuildOptions(options *buildOptions) error {
	options.rpcAddress = strings.TrimSpace(options.rpcAddress)
	if options.rpcAddress != "" && options.rpcTimeout <= 0 {
		return errors.New("-rpc-timeout must be positive")
	}
	return nil
}

type buildContext struct {
	locations   *workspace.SourceLocations
	environment commandEnvironment
	options     buildOptions
	module      *workspace.Directory
	sources     workspace.SourceSet
	roots       []string
	sourceRoot  string
	explicit    bool
	cache       *cache.DiskBackend
	cacheConfig compilerCacheSettings
}

func loadBuildContext(environment commandEnvironment, options buildOptions, operands []string) (*buildContext, error) {
	explicit := false
	for _, operand := range operands {
		if strings.HasSuffix(operand, ".go") {
			return nil, fmt.Errorf("source file %q must use .mgo", operand)
		}
		isSource := strings.HasSuffix(operand, ".mgo")
		if isSource {
			explicit = true
		} else if explicit {
			return nil, errors.New("source files and package patterns cannot be mixed")
		}
	}
	if explicit {
		for _, operand := range operands {
			if !strings.HasSuffix(operand, ".mgo") {
				return nil, errors.New("source files and package patterns cannot be mixed")
			}
		}
	}

	library := stdlib.Open()
	standardSources, err := workspace.StandardLibrary(library)
	if err != nil {
		return nil, err
	}
	var leadingSources []workspace.SourceSet
	locations := &workspace.SourceLocations{}
	var module *workspace.Directory
	var roots []string
	var sourceRoot string
	if explicit {
		files, directory, loadErr := loadExplicitSourceFiles(environment.workingDir, operands, target.Target{Tags: options.tags})
		if loadErr != nil {
			return nil, loadErr
		}
		leadingSources = append(leadingSources, files)
		roots = []string{workspace.CommandLinePackage}
		sourceRoot = directory
		additional, loadErr := options.sources.load(environment, true)
		if loadErr != nil {
			return nil, loadErr
		}
		if additional.Sources != nil {
			locations = additional.Locations
			leadingSources = append(leadingSources, additional.Sources)
		}
		if err := locations.Add(directory, files); err != nil {
			return nil, err
		}
	} else {
		foundModule, loadErr := options.sources.load(environment, false)
		if loadErr != nil {
			return nil, loadErr
		}
		module = &foundModule
		locations = module.Locations
		roots, loadErr = workspace.SelectPackages(module.Root, module.Root, module.ModulePath, module.Documents, operands)
		if loadErr != nil {
			return nil, loadErr
		}
		sourceRoot = module.Root
	}
	if module != nil {
		leadingSources = append(leadingSources, module.Sources)
	}
	leadingSources = append(leadingSources, standardSources)
	sources, err := workspace.MergeSourceSets(leadingSources...)
	if err != nil {
		return nil, err
	}
	cacheConfig, err := environment.compilerCacheSettings()
	if err != nil {
		return nil, err
	}
	backend := cache.NewDiskBackend(cacheConfig.root)
	if err := backend.Trim(); err != nil {
		return nil, err
	}
	return &buildContext{
		locations: locations, environment: environment, options: options, module: module, sources: sources,
		roots: roots, sourceRoot: sourceRoot, explicit: explicit, cache: backend, cacheConfig: cacheConfig,
	}, nil
}

func (build *buildContext) openCompiler() (*compiler.Compiler, error) {
	session, err := compiler.New(compiler.Options{
		Sources: build.sources, Target: target.Target{Tags: append([]string(nil), build.options.tags...)}, Cache: cache.New(build.cache),
		CacheVerify: build.cacheConfig.verify, CacheHash: build.cacheConfig.hash,
		TraceCache:   newCacheTrace(build.cacheConfig, build.options.cacheLog),
		Optimization: build.options.optimization,
		Symbols:      build.options.symbols,
	})
	return session, err
}

func (build *buildContext) openEndpoint(ctx context.Context) (*rpc.Endpoint, error) {
	if build.options.rpcAddress == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	remoteCtx, cancel := context.WithTimeout(ctx, build.options.rpcTimeout)
	defer cancel()
	endpoint, err := rpcgateway.Dial(remoteCtx, build.options.rpcAddress, rpcgateway.DialOptions{})
	if err != nil {
		return nil, err
	}
	return endpoint, nil
}

func newCacheTrace(settings compilerCacheSettings, writer io.Writer) func(cache.Event) {
	if !settings.trace && !settings.hash {
		return nil
	}
	if writer == nil {
		writer = io.Discard
	}
	return func(event cache.Event) {
		if len(event.MaterialJSON) != 0 {
			if settings.hash {
				fmt.Fprintf(writer, "cache %s %s %s\n%s\n", event.Kind, event.ModulePath, event.ActionKey, event.MaterialJSON)
			}
			return
		}
		if settings.trace {
			fmt.Fprintf(writer, "cache %s %s action=%s artifact=%s %s\n", event.Kind, event.ModulePath, event.ActionKey, event.ArtifactHash, event.Reason)
		}
	}
}

func writeDiagnostics(writer io.Writer, diagnostics []source.Diagnostic) {
	if writer == nil {
		return
	}
	for _, diagnostic := range diagnostics {
		location := diagnostic.Primary.Start
		if location.File != "" && location.Line > 0 {
			fmt.Fprintf(writer, "%s:%d:%d: %s: %s\n", location.File, location.Line, location.Column, diagnostic.Code, diagnostic.Message)
		} else {
			fmt.Fprintf(writer, "%s: %s\n", diagnostic.Code, diagnostic.Message)
		}
	}
}

type stringList []string

func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty value")
	}
	*values = append(*values, value)
	return nil
}

func (values *stringList) String() string { return strings.Join(*values, ",") }
