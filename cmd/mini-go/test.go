package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
	runtimepkg "github.com/d7z-team/mini-go/runtime"
)

type testCommandOptions struct {
	build        buildOptions
	run          string
	skip         string
	count        int
	changedFiles stringList
}

func runTestsContext(environment commandEnvironment, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mini-go test", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := testCommandOptions{build: buildOptions{cacheLog: stderr, rpcTimeout: 10 * time.Second, optimization: compiler.OptimizationDefault}, run: ".", count: 1}
	options.build.sources.flags(flags)
	flags.Var(&options.build.tags, "tag", "enable a Mini-Go build tag; repeatable")
	optimization := int(options.build.optimization)
	flags.IntVar(&optimization, "O", optimization, "compiler optimization level: 0, 1, or 2")
	flags.BoolVar(&options.build.symbols, "symbols", false, "generate detached program symbols")
	addRPCFlags(flags, &options.build)
	flags.StringVar(&options.run, "run", options.run, "run tests matching the regular expression")
	flags.StringVar(&options.skip, "skip", "", "skip tests matching the regular expression")
	flags.IntVar(&options.count, "count", options.count, "execute each selected package test image this many times")
	flags.Var(&options.changedFiles, "changed-file", "select tests affected by a working-directory-relative source file; repeatable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := normalizeBuildOptions(&options.build); err != nil {
		return err
	}
	level, err := compiler.ParseOptimizationLevel(optimization)
	if err != nil {
		return err
	}
	options.build.optimization = level
	if options.count < 1 {
		return errors.New("-count must be at least 1")
	}
	runPattern, err := regexp.Compile(options.run)
	if err != nil {
		return fmt.Errorf("invalid -run expression: %w", err)
	}
	var skipPattern *regexp.Regexp
	if options.skip != "" {
		skipPattern, err = regexp.Compile(options.skip)
		if err != nil {
			return fmt.Errorf("invalid -skip expression: %w", err)
		}
	}
	build, err := loadBuildContext(environment, options.build, flags.Args())
	if err != nil {
		return err
	}
	if build.module == nil || build.explicit {
		return errors.New("test accepts directory package patterns")
	}
	packagePaths := build.roots
	changed := append([]string(nil), options.changedFiles...)
	if len(changed) != 0 {
		refs := make([]workspace.ChangedFile, 0, len(changed))
		for _, name := range changed {
			absolute := environment.path(name)
			name, err = filepath.Rel(build.module.Root, absolute)
			if err != nil {
				return err
			}
			name = filepath.ToSlash(filepath.Clean(name))
			if name == "." || name == ".." || strings.HasPrefix(name, "../") {
				return fmt.Errorf("changed file %q is outside the workspace", name)
			}
			dir := filepath.ToSlash(filepath.Dir(name))
			modulePath := build.module.ModulePath
			if dir != "." {
				modulePath += "/" + dir
			}
			refs = append(refs, workspace.ChangedFile{ModulePath: modulePath, Path: name})
		}
		affected, err := workspace.AffectedTestPackages(build.sources, refs, target.Target{Tags: options.build.tags})
		if err != nil {
			return err
		}
		allowed := make(map[string]struct{}, len(affected))
		for _, modulePath := range affected {
			allowed[modulePath] = struct{}{}
		}
		selected := packagePaths[:0]
		for _, modulePath := range packagePaths {
			if _, ok := allowed[modulePath]; ok {
				selected = append(selected, modulePath)
			}
		}
		packagePaths = selected
	}
	session, err := build.openCompiler()
	if err != nil {
		return err
	}
	endpoint, err := build.openEndpoint(environment.context())
	if err != nil {
		return err
	}
	if endpoint != nil {
		defer endpoint.Close()
	}
	testPackages := make([]string, 0, len(packagePaths))
	noTestPackages := make([]string, 0, len(packagePaths))
	for _, modulePath := range packagePaths {
		pkg, exists, err := build.module.Sources.Package(modulePath)
		if err != nil || !exists {
			return fmt.Errorf("load package %q: exists=%v err=%v", modulePath, exists, err)
		}
		selected, diagnostics, err := workspace.SelectPackage(pkg, target.Target{Tags: options.build.tags})
		if err != nil {
			return err
		}
		if len(diagnostics) == 0 && len(selected.TestFiles) == 0 {
			noTestPackages = append(noTestPackages, modulePath)
			continue
		}
		testPackages = append(testPackages, modulePath)
	}
	failed := false
	for _, modulePath := range noTestPackages {
		compiled, err := session.Compile(modulePath)
		if err != nil {
			return err
		}
		if !compiled.OK() {
			writeDiagnostics(stderr, compiled.Diagnostics)
			failed = true
			continue
		}
		fmt.Fprintf(stdout, "?\t%s\t[no test files]\n", modulePath)
	}
	preparedTests := map[string]compiler.PrepareResult{}
	if len(testPackages) != 0 {
		preparedTests, err = session.PrepareTests(testPackages)
		if err != nil {
			return err
		}
	}
	for _, modulePath := range testPackages {
		prepared := preparedTests[modulePath]
		if !prepared.Checked.OK() || prepared.Image == nil {
			for _, diagnostic := range prepared.Checked.Diagnostics {
				fmt.Fprintf(stderr, "%s: %s\n", diagnostic.Code, diagnostic.Message)
			}
			failed = true
			continue
		}
		indexes := make([]int, 0, len(prepared.TestManifest))
		for _, test := range prepared.TestManifest {
			if runPattern.MatchString(test.Name) && (skipPattern == nil || !skipPattern.MatchString(test.Name)) {
				indexes = append(indexes, test.Index)
			}
		}
		program, err := runtimepkg.LoadExecutionImage(*prepared.Image)
		if err != nil {
			return err
		}
		if prepared.Symbols != nil {
			program, err = program.WithSymbols(*prepared.Symbols)
			if err != nil {
				return err
			}
		}
		for iteration := 0; iteration < options.count; iteration++ {
			values := make([]runtimepkg.HostValue, len(indexes))
			for i, index := range indexes {
				values[i] = runtimepkg.HostInt("Int", int64(index))
			}
			runtimeOptions, cleanup, err := commandRuntimeOptions(environment.workingDir, environment.stdin, stdout, stderr, program, endpoint)
			if err != nil {
				return err
			}
			instance, err := program.Instantiate(environment.context(), runtimeOptions)
			if err != nil {
				_ = cleanup(context.Background())
				return err
			}
			result, err := instance.Call(environment.context(), compiler.TestSelectionEntry, runtimepkg.HostSlice("Slice<Int>", values...))
			closeErr := instance.Close()
			cleanupErr := cleanup(context.Background())
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = cleanupErr
			}
			if err != nil {
				fmt.Fprintf(stderr, "FAIL\t%s\t%s\n", modulePath, err)
				failed = true
				continue
			}
			passed, failures, err := parseTestReport(result)
			if err != nil {
				return fmt.Errorf("read %s test report: %w", modulePath, err)
			}
			if passed {
				fmt.Fprintf(stdout, "ok\t%s\n", modulePath)
			} else {
				for _, failure := range failures {
					fmt.Fprintf(stderr, "--- FAIL: %s\n", failure)
				}
				fmt.Fprintf(stderr, "FAIL\t%s\n", modulePath)
				failed = true
			}
		}
	}
	if failed {
		return errors.New("tests failed")
	}
	return nil
}
