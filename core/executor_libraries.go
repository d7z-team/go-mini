package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func normalizeSurfaceLibraryModule(module surface.LibraryModule) (surface.LibraryModule, error) {
	path := strings.TrimSpace(module.Path)
	if path == "" {
		return surface.LibraryModule{}, errors.New("surface library missing module path")
	}
	if len(module.Files) == 0 {
		return surface.LibraryModule{}, fmt.Errorf("surface library %s has no source files", path)
	}
	goLanguage := (compiler.GoFrontend{}).Language()
	files := make([]surface.LibraryFile, len(module.Files))
	for i, file := range module.Files {
		language := strings.TrimSpace(file.Language)
		if language == "" || language == "go" {
			language = goLanguage
		}
		if language != goLanguage {
			return surface.LibraryModule{}, fmt.Errorf("surface library %s file %d uses unsupported language %s", path, i, language)
		}
		filename := strings.TrimSpace(file.Filename)
		if filename == "" {
			filename = fmt.Sprintf("%s_%d%s", strings.ReplaceAll(path, "/", "_"), i, compiler.ScriptFileExt)
		}
		files[i] = surface.LibraryFile{
			Filename: filename,
			Language: language,
			Code:     file.Code,
		}
	}
	return surface.LibraryModule{Path: path, Files: files}, nil
}

func parseSurfaceLibraryModule(module surface.LibraryModule) (*ast.ProgramStmt, error) {
	files := make([]compiler.SourceFile, len(module.Files))
	for i, file := range module.Files {
		files[i] = compiler.SourceFile{
			Filename: file.Filename,
			Language: file.Language,
			Code:     file.Code,
		}
	}
	programs, _, err := compiler.ParseSourceFiles(files, false)
	if err != nil {
		return nil, fmt.Errorf("parse surface library %s: %w", module.Path, err)
	}
	program, err := compiler.MergePrograms(programs)
	if err != nil {
		return nil, fmt.Errorf("merge surface library %s: %w", module.Path, err)
	}
	return program, nil
}

func prepareSurfaceLibraryModules(modules []surface.LibraryModule) ([]surface.LibraryModule, map[string]*ast.ProgramStmt, map[string]string, error) {
	if len(modules) == 0 {
		return nil, nil, nil, nil
	}
	out := make([]surface.LibraryModule, 0, len(modules))
	asts := make(map[string]*ast.ProgramStmt, len(modules))
	hashes := make(map[string]string, len(modules))
	for _, module := range modules {
		normalized, err := normalizeSurfaceLibraryModule(module)
		if err != nil {
			return nil, nil, nil, err
		}
		hash := normalized.Hash()
		if existing := hashes[normalized.Path]; existing != "" {
			if existing != hash {
				return nil, nil, nil, fmt.Errorf("surface library %s conflicts with existing source", normalized.Path)
			}
			continue
		}
		program, err := parseSurfaceLibraryModule(normalized)
		if err != nil {
			return nil, nil, nil, err
		}
		hashes[normalized.Path] = hash
		asts[normalized.Path] = program
		out = append(out, normalized)
	}
	return out, asts, hashes, nil
}

func resolveSurfaceLibraryHashes(asts map[string]*ast.ProgramStmt, sourceHashes map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(sourceHashes))
	visiting := make(map[string]bool, len(sourceHashes))

	var visit func(string) (string, error)
	visit = func(path string) (string, error) {
		if hash := resolved[path]; hash != "" {
			return hash, nil
		}
		sourceHash := sourceHashes[path]
		if sourceHash == "" {
			return "", fmt.Errorf("surface library %s missing source hash", path)
		}
		if visiting[path] {
			return "", fmt.Errorf("surface library %s has circular import dependency", path)
		}
		visiting[path] = true
		program := asts[path]
		parts := []string{"vm-library-resolved", path, sourceHash}
		if program != nil {
			imports := make([]string, 0, len(program.Imports))
			for _, imp := range program.Imports {
				importPath := strings.TrimSpace(imp.Path)
				if importPath != "" && sourceHashes[importPath] != "" {
					imports = append(imports, importPath)
				}
			}
			sort.Strings(imports)
			for _, importPath := range imports {
				depHash, err := visit(importPath)
				if err != nil {
					return "", err
				}
				parts = append(parts, importPath, depHash)
			}
		}
		visiting[path] = false
		hash := runtime.VersionedExternalRequirementHash(parts...)
		resolved[path] = hash
		return hash, nil
	}

	for path := range sourceHashes {
		if _, err := visit(path); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func (e *MiniExecutor) validateSurfaceLibrariesLocked(modules []surface.LibraryModule, hashes map[string]string) error {
	for _, module := range modules {
		path := module.Path
		hash := hashes[path]
		if existing := e.librarySourceHashes[path]; existing != "" {
			if existing != hash {
				return fmt.Errorf("surface library %s conflicts with existing source", path)
			}
			continue
		}
		if prepared := e.modules[path]; prepared != nil {
			return fmt.Errorf("surface library %s conflicts with registered bytecode module", path)
		}
		if existing := e.moduleSources[path]; existing != nil {
			return fmt.Errorf("surface library %s conflicts with registered source module", path)
		}
	}
	return nil
}

func (e *MiniExecutor) applySurfaceLibrariesLocked(modules []surface.LibraryModule, sourceHashes, resolvedHashes map[string]string) {
	for _, module := range modules {
		path := module.Path
		e.librarySourceHashes[path] = sourceHashes[path]
		e.sourceLibraries[path] = module
		delete(e.moduleSources, path)
		delete(e.modules, path)
	}
	e.libraryHashes = resolvedHashes
}

func (e *MiniExecutor) prepareModuleFromSource(path string) (*runtime.PreparedProgram, error) {
	e.mu.RLock()
	if prepared := e.modules[path]; prepared != nil {
		e.mu.RUnlock()
		return prepared, nil
	}
	library, hasLibrary := e.sourceLibraries[path]
	program := e.moduleSources[path]
	e.mu.RUnlock()
	if hasLibrary {
		var err error
		program, err = parseSurfaceLibraryModule(library)
		if err != nil {
			return nil, err
		}
	} else if program == nil {
		return nil, fmt.Errorf("%w: %s", runtime.ErrModuleNotFound, path)
	}

	compiled, _, err := e.newCompiler().CompileProgram(path, "", program, false)
	if err != nil {
		return nil, fmt.Errorf("compile module %s: %w", path, err)
	}
	if compiled == nil || compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		return nil, fmt.Errorf("module %s did not produce executable bytecode", path)
	}

	prepared := compiled.Bytecode.Executable
	e.mu.Lock()
	e.modules[path] = prepared
	if !hasLibrary {
		e.moduleSources[path] = program
	}
	e.mu.Unlock()

	if err := e.compileImportedModules(compiled.Program, compiled.ImportedPrograms, map[string]bool{path: true}); err != nil {
		return nil, err
	}
	return prepared, nil
}

func cloneSurfaceLibraryHashes(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for path, hash := range in {
		out[path] = hash
	}
	return out
}

func (e *MiniExecutor) recomputeSurfaceLibraryHashesLocked() {
	if len(e.librarySourceHashes) == 0 {
		e.libraryHashes = make(map[string]string)
		return
	}
	asts := make(map[string]*ast.ProgramStmt, len(e.librarySourceHashes))
	for path := range e.librarySourceHashes {
		library, ok := e.sourceLibraries[path]
		if !ok {
			continue
		}
		program, err := parseSurfaceLibraryModule(library)
		if err != nil {
			e.libraryHashes = cloneSurfaceLibraryHashes(e.librarySourceHashes)
			return
		}
		asts[path] = program
	}
	hashes, err := resolveSurfaceLibraryHashes(asts, e.librarySourceHashes)
	if err != nil {
		e.libraryHashes = cloneSurfaceLibraryHashes(e.librarySourceHashes)
		return
	}
	e.libraryHashes = hashes
}
