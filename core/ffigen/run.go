package ffigen

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Run executes ffigen using the same input model as the CLI.
func Run(opts Options) error {
	return NewGenerator(opts).Run()
}

func (g *Generator) Run() error {
	if g.opts.PackageName == "" || g.opts.Output == "" {
		return errors.New("usage: ffigen -pkg <name> -out <file> [input files...]")
	}

	if len(g.opts.Args) == 0 {
		return errors.New("no input files provided")
	}

	mode, err := detectGenerationMode(g.opts.Args)
	if err != nil {
		return err
	}
	switch mode {
	case modeDirectory:
		return g.runDirectoryMode(g.opts.Args[0])
	case modeFiles:
		return g.runFileMode(g.opts.Args)
	default:
		return errors.New("unsupported generation mode")
	}
}

func detectGenerationMode(args []string) (generationMode, error) {
	if len(args) == 0 {
		return modeFiles, errors.New("no input provided")
	}
	hasDir := false
	hasFile := false
	for _, arg := range args {
		if strings.HasSuffix(strings.ToLower(arg), ".go") && isGeneratedFilename(arg) {
			return modeFiles, fmt.Errorf("generated file %s cannot be used as input", arg)
		}
		info, err := os.Stat(arg)
		if err != nil {
			return modeFiles, err
		}
		if info.IsDir() {
			hasDir = true
		} else {
			hasFile = true
		}
	}
	if hasDir && hasFile {
		return modeFiles, errors.New("cannot mix directories and files")
	}
	if hasDir {
		if len(args) != 1 {
			return modeFiles, errors.New("directory mode accepts exactly one directory")
		}
		return modeDirectory, nil
	}
	return modeFiles, nil
}

func (g *Generator) runDirectoryMode(dir string) error {
	info, err := os.Stat(g.opts.Output)
	if err == nil && !info.IsDir() {
		return errors.New("directory mode requires -out to be a directory")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(g.opts.Output, 0o755); err != nil {
		return err
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := g.initPackagePath(absDir); err != nil {
		return err
	}

	files, err := g.parseDirectoryFiles(absDir, "")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no non-generated go files found in %s", dir)
	}

	pkgModules, pkgData := g.collectPackageData(files, files)
	if len(pkgModules) > 1 {
		return errors.New("directory mode allows at most one ffigen:module per package")
	}
	outputPath := filepath.Join(g.opts.Output, fmt.Sprintf("ffigen_%s.go", g.opts.PackageName))
	return g.generatePackageOutput(outputPath, pkgData)
}

func (g *Generator) runFileMode(args []string) error {
	firstDir := filepath.Dir(args[0])
	if err := g.initPackagePath(firstDir); err != nil {
		return err
	}
	reservedName := fmt.Sprintf("ffigen_%s.go", g.opts.PackageName)
	if filepath.Base(g.opts.Output) == reservedName {
		return fmt.Errorf("file mode cannot write reserved package output name %s", reservedName)
	}
	allFiles, inputFiles, err := g.parseFileInputs(args, g.opts.Output)
	if err != nil {
		return err
	}
	_, pkgData := g.collectPackageData(allFiles, inputFiles)
	return g.generatePackageOutput(g.opts.Output, pkgData)
}

func (g *Generator) initPackagePath(dir string) error {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}|{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}{{end}}")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("deriving import path in %s: %w", dir, err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	g.packagePath = parts[0]
	if g.packagePath == "" || g.packagePath == "." {
		return errors.New("derived import path is empty or invalid")
	}
	if len(parts) >= 3 {
		g.modulePath = parts[1]
		g.moduleDir = parts[2]
	}
	return nil
}
