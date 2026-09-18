package runtime

import (
	"errors"
	"fmt"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

var ErrDebugSymbolsUnavailable = errors.New("debug symbols are unavailable")

type symbolIndex struct {
	hash     string
	packages map[string]packageSymbolIndex
}

type packageSymbolIndex struct {
	files     map[string]string
	functions map[string]ir.FunctionSymbols
	globals   map[string]string
}

func newSymbolIndex(code *programCode, symbols *ir.ProgramSymbols) (*symbolIndex, error) {
	if code == nil || symbols == nil {
		return nil, errors.New("program and symbols are required")
	}
	if symbols.Format != ir.SymbolsFormat || symbols.Version != ir.SymbolsVersion || symbols.ContractID != ir.SymbolsContract ||
		symbols.CompilerID != code.image.CompilerID || symbols.ProgramHash != code.image.Hash {
		return nil, errors.New("program symbols identity mismatch")
	}
	if len(symbols.Packages) != len(code.modules) {
		return nil, errors.New("program symbols package closure mismatch")
	}
	for modulePath, executable := range code.modules {
		pkg, ok := symbols.Packages[modulePath]
		if !ok {
			return nil, fmt.Errorf("program symbols missing package %q", modulePath)
		}
		if err := ir.ValidatePackageSymbols(&executable.Artifact, code.packageHashes[modulePath], &pkg); err != nil {
			return nil, fmt.Errorf("validate package symbols %q: %w", modulePath, err)
		}
	}
	hash, err := ir.HashProgramSymbols(*symbols)
	if err != nil || symbols.Hash != hash {
		return nil, errors.New("program symbols hash mismatch")
	}
	owned := ir.CloneProgramSymbols(*symbols)
	index := &symbolIndex{hash: owned.Hash, packages: make(map[string]packageSymbolIndex, len(owned.Packages))}
	for modulePath, pkg := range owned.Packages {
		packageIndex := packageSymbolIndex{
			files:     make(map[string]string, len(pkg.Files)),
			functions: make(map[string]ir.FunctionSymbols, len(pkg.Functions)),
			globals:   make(map[string]string, len(pkg.Globals)),
		}
		for _, file := range pkg.Files {
			packageIndex.files[file.Path] = file.Hash
		}
		for _, function := range pkg.Functions {
			packageIndex.functions[function.ID] = function
		}
		for _, global := range pkg.Globals {
			packageIndex.globals[global.ID] = global.Name
		}
		index.packages[modulePath] = packageIndex
	}
	return index, nil
}

func (s *symbolIndex) sourceHash(modulePath, file string) string {
	if s == nil {
		return ""
	}
	return s.packages[modulePath].files[file]
}

func (s *symbolIndex) function(modulePath, functionID string) (ir.FunctionSymbols, bool) {
	if s == nil {
		return ir.FunctionSymbols{}, false
	}
	pkg, ok := s.packages[modulePath]
	if !ok {
		return ir.FunctionSymbols{}, false
	}
	function, ok := pkg.functions[functionID]
	return function, ok
}

func (s *symbolIndex) locations(modulePath, functionID string, pc int) []ir.Location {
	function, ok := s.function(modulePath, functionID)
	if !ok {
		return nil
	}
	for _, location := range function.Locations {
		if location.PC == pc {
			return location.Points
		}
		if location.PC > pc {
			break
		}
	}
	return nil
}

func (s *symbolIndex) nearestLocation(modulePath, functionID string, pc int) (ir.Location, bool) {
	function, ok := s.function(modulePath, functionID)
	if !ok || pc < 0 {
		return ir.Location{}, false
	}
	for index := len(function.Locations) - 1; index >= 0; index-- {
		location := function.Locations[index]
		if location.PC <= pc && len(location.Points) != 0 {
			return location.Points[0], true
		}
	}
	return ir.Location{}, false
}

func (s *symbolIndex) globalName(modulePath, globalID string) string {
	if s == nil {
		return ""
	}
	return s.packages[modulePath].globals[globalID]
}
