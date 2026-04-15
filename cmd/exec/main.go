package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	engine "gopkg.d7z.net/go-mini/core"
)

type execOptions struct {
	disassemble bool
	run         bool
	bytecode    string
	output      string
	inputs      []string
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
	flags.StringVar(&options.bytecode, "bytecode", "", "load bytecode JSON instead of script sources")
	flags.StringVar(&options.output, "o", "", "write compiled bytecode JSON to file")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [options] <dir>\n", os.Args[0])
		fmt.Fprintf(flags.Output(), "   or: %s [options] <file1%s> [file2%s ...]\n", os.Args[0], engine.SourceFileExt(), engine.SourceFileExt())
		fmt.Fprintln(flags.Output(), "   or:", os.Args[0], "[options] -bytecode <program.json>")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	options.inputs = flags.Args()

	if options.bytecode != "" {
		if len(options.inputs) > 0 {
			return nil, errors.New("cannot mix -bytecode with source inputs")
		}
		if options.output != "" {
			return nil, errors.New("cannot use -o when loading bytecode")
		}
		if !options.run && !options.disassemble {
			options.run = true
		}
		return options, nil
	}

	if len(options.inputs) == 0 {
		flags.Usage()
		return nil, errors.New("missing input path")
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

	if len(options.inputs) == 1 {
		info, err := os.Stat(options.inputs[0])
		if err == nil && info.IsDir() {
			return executor.NewRuntimeByDir(options.inputs[0])
		}
	}

	files, err := loadSourceFiles(options.inputs)
	if err != nil {
		return nil, err
	}
	return executor.NewRuntimeByFiles(files)
}

func loadSourceFiles(files []string) ([]engine.SourceFile, error) {
	res := make([]engine.SourceFile, 0, len(files))
	for _, name := range files {
		absolutePath, err := filepath.Abs(name)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", name, err)
		}
		sourceBytes, err := os.ReadFile(absolutePath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		res = append(res, engine.SourceFile{
			Filename: name,
			Code:     string(sourceBytes),
		})
	}
	if len(res) == 0 {
		return nil, errors.New("no source files loaded")
	}
	return res, nil
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
