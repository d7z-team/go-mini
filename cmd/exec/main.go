package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type execOptions struct {
	disassemble bool
	run         bool
	bytecode    string
	output      string
	files       []string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}

	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	program, err := loadProgram(executor, options)
	if err != nil {
		return err
	}

	if options.output != "" {
		if err := writeBytecode(program, options.output); err != nil {
			return err
		}
	}

	if options.disassemble {
		fmt.Println(program.Disassemble())
		return nil
	}
	if !options.run {
		return nil
	}
	return program.Execute(context.Background())
}

func parseOptions(args []string) (*execOptions, error) {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	options := &execOptions{}
	flags.BoolVar(&options.disassemble, "d", false, "print disassembly and exit")
	flags.BoolVar(&options.run, "run", false, "run after loading or compiling")
	flags.StringVar(&options.bytecode, "bytecode", "", "load bytecode JSON instead of Go source files")
	flags.StringVar(&options.output, "o", "", "write compiled bytecode JSON to file")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [options] <file1.go> [file2.go ...]\n", os.Args[0])
		fmt.Fprintln(flags.Output(), "   or:", os.Args[0], "[options] -bytecode <program.json>")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	options.files = flags.Args()

	if options.bytecode != "" {
		if len(options.files) > 0 {
			return nil, fmt.Errorf("cannot mix -bytecode with source files")
		}
		if options.output != "" {
			return nil, fmt.Errorf("cannot use -o when loading bytecode")
		}
		if !options.run && !options.disassemble {
			options.run = true
		}
		return options, nil
	}

	if len(options.files) == 0 {
		flags.Usage()
		return nil, fmt.Errorf("missing input files")
	}
	if options.output == "" && !options.disassemble {
		options.run = true
	}
	return options, nil
}

func loadProgram(executor *engine.MiniExecutor, options *execOptions) (*engine.MiniProgram, error) {
	if options.bytecode != "" {
		payload, err := os.ReadFile(options.bytecode)
		if err != nil {
			return nil, fmt.Errorf("read bytecode %s: %w", options.bytecode, err)
		}
		return executor.NewRuntimeByBytecodeJSON(payload)
	}

	rootProgram, err := loadSourceProgram(options.files)
	if err != nil {
		return nil, err
	}
	return executor.NewRuntimeByProgram(rootProgram)
}

func loadSourceProgram(files []string) (*ast.ProgramStmt, error) {
	converter := ffigo.NewGoToASTConverter()
	var rootProgram *ast.ProgramStmt
	for index, name := range files {
		absolutePath, err := filepath.Abs(name)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", name, err)
		}
		sourceBytes, err := os.ReadFile(absolutePath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		node, err := converter.ConvertSource(name, string(sourceBytes))
		if err != nil {
			return nil, fmt.Errorf("convert %s: %w", name, err)
		}
		program, ok := node.(*ast.ProgramStmt)
		if !ok {
			return nil, fmt.Errorf("unexpected root node type for %s: %T", name, node)
		}
		if index == 0 {
			rootProgram = program
			continue
		}
		if rootProgram.Package != program.Package {
			return nil, fmt.Errorf("package mismatch: %s vs %s", rootProgram.Package, program.Package)
		}
		mergeProgram(rootProgram, program)
	}
	if rootProgram == nil {
		return nil, fmt.Errorf("no source program loaded")
	}
	return rootProgram, nil
}

func writeBytecode(program *engine.MiniProgram, outputPath string) error {
	payload, err := program.MarshalIndentBytecodeJSON("", "  ")
	if err != nil {
		return fmt.Errorf("marshal bytecode: %w", err)
	}
	if err := os.WriteFile(outputPath, payload, 0o644); err != nil {
		return fmt.Errorf("write bytecode %s: %w", outputPath, err)
	}
	return nil
}

func mergeProgram(destination, source *ast.ProgramStmt) {
	for name, function := range source.Functions {
		destination.Functions[name] = function
	}
	for name, structNode := range source.Structs {
		destination.Structs[name] = structNode
	}
	for name, variable := range source.Variables {
		destination.Variables[name] = variable
	}
	for name, constant := range source.Constants {
		destination.Constants[name] = constant
	}
	for name, iface := range source.Interfaces {
		destination.Interfaces[name] = iface
	}
	destination.Main = append(destination.Main, source.Main...)
	destination.Imports = append(destination.Imports, source.Imports...)
}
