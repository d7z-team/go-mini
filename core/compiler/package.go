package compiler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

const ScriptFileExt = ".mgo"

type SourceFile struct {
	Filename string
	Code     string
}

func CompileDirInputs(dir string) ([]SourceFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	files := make([]SourceFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ScriptFileExt {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		files = append(files, SourceFile{
			Filename: path,
			Code:     string(content),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no %s source files found in %s", ScriptFileExt, dir)
	}
	return files, nil
}

func ParseSourceFiles(files []SourceFile, tolerant bool) ([]*ast.ProgramStmt, []error, error) {
	if len(files) == 0 {
		return nil, nil, errors.New("missing source files")
	}

	converter := ffigo.NewGoToASTConverter()
	programs := make([]*ast.ProgramStmt, 0, len(files))
	var allErrs []error

	for _, file := range files {
		var (
			node ast.Node
			errs []error
			err  error
		)
		if tolerant {
			node, errs = converter.ConvertSourceTolerant(file.Filename, file.Code)
			allErrs = append(allErrs, errs...)
		} else {
			node, err = converter.ConvertSource(file.Filename, file.Code)
			if err != nil {
				return nil, nil, fmt.Errorf("convert %s: %w", file.Filename, err)
			}
		}

		if node == nil {
			return nil, allErrs, fmt.Errorf("failed to parse source %s", file.Filename)
		}
		program, ok := node.(*ast.ProgramStmt)
		if !ok {
			return nil, allErrs, fmt.Errorf("unexpected root node type for %s: %T", file.Filename, node)
		}
		programs = append(programs, program)
	}

	return programs, allErrs, nil
}

func MergePrograms(programs []*ast.ProgramStmt) (*ast.ProgramStmt, error) {
	if len(programs) == 0 {
		return nil, errors.New("missing programs")
	}

	root := programs[0]
	if root == nil {
		return nil, errors.New("invalid root program")
	}

	for _, program := range programs[1:] {
		if program == nil {
			return nil, errors.New("invalid merged program")
		}
		if root.Package != program.Package {
			return nil, fmt.Errorf("package mismatch: %s vs %s", root.Package, program.Package)
		}
		if err := mergeProgram(root, program); err != nil {
			return nil, err
		}
	}

	return root, nil
}

func mergeProgram(dest, src *ast.ProgramStmt) error {
	destImports := importAliasPaths(dest.Imports)
	srcImports := importAliasPaths(src.Imports)
	if len(dest.Imports) == 0 {
		dest.Imports = append(dest.Imports, src.Imports...)
	} else {
		seen := make(map[string]struct{}, len(dest.Imports))
		for _, imp := range dest.Imports {
			seen[imp.Alias+"|"+imp.Path] = struct{}{}
		}
		for _, imp := range src.Imports {
			key := imp.Alias + "|" + imp.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			dest.Imports = append(dest.Imports, imp)
		}
	}
	for k, v := range src.Functions {
		if _, exists := dest.Functions[k]; exists {
			return fmt.Errorf("duplicate function definition: %s", k)
		}
		dest.Functions[k] = v
	}
	for k, v := range src.Structs {
		if _, exists := dest.Structs[k]; exists {
			return fmt.Errorf("duplicate struct definition: %s", k)
		}
		dest.Structs[k] = v
	}
	for k, v := range src.Variables {
		if _, exists := dest.Variables[k]; exists {
			if destImports[string(k)] != "" && destImports[string(k)] == srcImports[string(k)] {
				continue
			}
			return fmt.Errorf("duplicate variable definition: %s", k)
		}
		dest.Variables[k] = v
	}
	for k, v := range src.Constants {
		if _, exists := dest.Constants[k]; exists {
			return fmt.Errorf("duplicate constant definition: %s", k)
		}
		dest.Constants[k] = v
	}
	for k, v := range src.Types {
		if _, exists := dest.Types[k]; exists {
			return fmt.Errorf("duplicate type definition: %s", k)
		}
		dest.Types[k] = v
	}
	for k, v := range src.Interfaces {
		if _, exists := dest.Interfaces[k]; exists {
			return fmt.Errorf("duplicate interface definition: %s", k)
		}
		dest.Interfaces[k] = v
	}
	dest.Main = append(dest.Main, src.Main...)
	return nil
}

func importAliasPaths(imports []ast.ImportSpec) map[string]string {
	res := make(map[string]string, len(imports))
	for _, imp := range imports {
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		res[alias] = imp.Path
	}
	return res
}
