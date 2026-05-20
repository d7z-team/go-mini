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
	resetRunState()
	*pkgName = opts.PackageName
	*outFile = opts.Output

	if *pkgName == "" || *outFile == "" {
		return errors.New("usage: ffigen -pkg <name> -out <file> [input files...]")
	}

	if len(opts.Args) == 0 {
		return errors.New("no input files provided")
	}

	mode, err := detectGenerationMode(opts.Args)
	if err != nil {
		return err
	}
	switch mode {
	case modeDirectory:
		return runDirectoryMode(opts.Args[0])
	case modeFiles:
		return runFileMode(opts.Args)
	default:
		return errors.New("unsupported generation mode")
	}
}

func resetRunState() {
	typeInfo = nil
	fset = nil
	knownImports = nil
	moduleCache = nil
	packagePath = ""
	modulePath = ""
	moduleDir = ""
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

func runDirectoryMode(dir string) error {
	info, err := os.Stat(*outFile)
	if err == nil && !info.IsDir() {
		return errors.New("directory mode requires -out to be a directory")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(*outFile, 0o755); err != nil {
		return err
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := initPackagePath(absDir); err != nil {
		return err
	}

	files, err := parseDirectoryFiles(absDir, "")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no non-generated go files found in %s", dir)
	}

	pkgModules, pkgData := collectPackageData(files, files)
	if len(pkgModules) > 1 {
		return errors.New("directory mode allows at most one ffigen:module per package")
	}
	outputPath := filepath.Join(*outFile, fmt.Sprintf("ffigen_%s.go", *pkgName))
	return generatePackageOutput(outputPath, pkgData)
}

func runFileMode(args []string) error {
	firstDir := filepath.Dir(args[0])
	if err := initPackagePath(firstDir); err != nil {
		return err
	}
	reservedName := fmt.Sprintf("ffigen_%s.go", *pkgName)
	if filepath.Base(*outFile) == reservedName {
		return fmt.Errorf("file mode cannot write reserved package output name %s", reservedName)
	}
	allFiles, inputFiles, err := parseFileInputs(args, *outFile)
	if err != nil {
		return err
	}
	_, pkgData := collectPackageData(allFiles, inputFiles)
	return generatePackageOutput(*outFile, pkgData)
}

func initPackagePath(dir string) error {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}|{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}{{end}}")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("deriving import path in %s: %w", dir, err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	packagePath = parts[0]
	if packagePath == "" || packagePath == "." {
		return errors.New("derived import path is empty or invalid")
	}
	if len(parts) >= 3 {
		modulePath = parts[1]
		moduleDir = parts[2]
	}
	return nil
}
