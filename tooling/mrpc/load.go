package mrpc

import (
	"errors"
	"fmt"
)

// SourceResolver loads the exact path used by one MRPC import.
type SourceResolver func(path string) (Source, error)

// LoadCatalog parses and validates one namespace and its transitive imports.
// Root sources may contain multiple files; each imported path resolves to one
// source file that may in turn import other contracts.
func LoadCatalog(roots []Source, resolve SourceResolver) (Catalog, []Diagnostic, error) {
	if len(roots) == 0 {
		return Catalog{}, nil, errors.New("MRPC catalog has no root sources")
	}
	rootFiles := make([]File, len(roots))
	var diagnostics []Diagnostic
	for index, source := range roots {
		rootFiles[index], diagnostics = Parse(source)
		if HasErrors(diagnostics) {
			return Catalog{}, diagnostics, nil
		}
	}
	if diagnostics = Validate(rootFiles); HasErrors(diagnostics) {
		return Catalog{}, diagnostics, nil
	}
	if resolve == nil {
		for _, file := range rootFiles {
			if len(file.Imports) != 0 {
				return Catalog{}, nil, errors.New("MRPC imports require a source resolver")
			}
		}
		return buildCatalog(rootFiles, nil), nil, nil
	}
	loaded := make(map[string]Catalog)
	visiting := make(map[string]bool)
	var load func(string) (Catalog, []Diagnostic, error)
	load = func(path string) (Catalog, []Diagnostic, error) {
		if catalog, ok := loaded[path]; ok {
			return catalog, nil, nil
		}
		if visiting[path] {
			return Catalog{}, nil, fmt.Errorf("MRPC import cycle includes %q", path)
		}
		visiting[path] = true
		defer delete(visiting, path)
		source, err := resolve(path)
		if err != nil {
			return Catalog{}, nil, fmt.Errorf("resolve MRPC import %q: %w", path, err)
		}
		file, parsed := Parse(source)
		if HasErrors(parsed) {
			return Catalog{}, parsed, nil
		}
		if checked := Validate([]File{file}); HasErrors(checked) {
			return Catalog{}, checked, nil
		}
		dependencies := make(map[string]Catalog, len(file.Imports))
		for _, imported := range file.Imports {
			dependency, checked, err := load(imported.Path)
			if err != nil || HasErrors(checked) {
				return Catalog{}, checked, err
			}
			dependencies[imported.Path] = dependency
		}
		catalog := buildCatalog([]File{file}, dependencies)
		if checked := ValidateDependencies(catalog, dependencies); HasErrors(checked) {
			return Catalog{}, checked, nil
		}
		loaded[path] = catalog
		return catalog, nil, nil
	}
	dependencies := make(map[string]Catalog)
	for _, file := range rootFiles {
		for _, imported := range file.Imports {
			dependency, checked, err := load(imported.Path)
			if err != nil || HasErrors(checked) {
				return Catalog{}, checked, err
			}
			dependencies[imported.Path] = dependency
		}
	}
	catalog := buildCatalog(rootFiles, dependencies)
	if diagnostics = ValidateDependencies(catalog, dependencies); HasErrors(diagnostics) {
		return Catalog{}, diagnostics, nil
	}
	return catalog, nil, nil
}
