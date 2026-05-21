package ffigen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type sourceImporter struct {
	fallback   types.Importer
	cache      map[string]*types.Package
	modulePath string
	moduleDir  string
}

func (g *Generator) newSourceImporter() types.Importer {
	return &sourceImporter{
		fallback:   importer.Default(),
		cache:      make(map[string]*types.Package),
		modulePath: g.modulePath,
		moduleDir:  g.moduleDir,
	}
}

func (i *sourceImporter) Import(path string) (*types.Package, error) {
	if pkg, ok := i.cache[path]; ok {
		return pkg, nil
	}
	if i.modulePath != "" && i.moduleDir != "" && (path == i.modulePath || strings.HasPrefix(path, i.modulePath+"/")) {
		pkg, err := i.importFromModule(path)
		if err == nil {
			i.cache[path] = pkg
			return pkg, nil
		}
	}
	pkg, err := i.fallback.Import(path)
	if err == nil {
		i.cache[path] = pkg
	}
	return pkg, err
}

func (i *sourceImporter) importFromModule(path string) (*types.Package, error) {
	// This importer only needs exported symbol names for selector type-checking.
	rel := strings.TrimPrefix(path, i.modulePath)
	rel = strings.TrimPrefix(rel, "/")
	dir := i.moduleDir
	if rel != "" {
		dir = filepath.Join(i.moduleDir, filepath.FromSlash(rel))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	localFset := token.NewFileSet()
	files := make([]*ast.File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filename := filepath.Join(dir, entry.Name())
		file, parseErr := parser.ParseFile(localFset, filename, nil, parser.ParseComments)
		if parseErr != nil || isGeneratedFile(entry.Name(), file) {
			continue
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no source files for import %s", path)
	}
	pkgName := files[0].Name.Name
	pkg := types.NewPackage(path, pkgName)
	scope := pkg.Scope()
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !s.Name.IsExported() || scope.Lookup(s.Name.Name) != nil {
							continue
						}
						obj := types.NewTypeName(s.Pos(), pkg, s.Name.Name, nil)
						_ = types.NewNamed(obj, types.NewStruct(nil, nil), nil)
						scope.Insert(obj)
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if !name.IsExported() || scope.Lookup(name.Name) != nil {
								continue
							}
							scope.Insert(types.NewVar(name.Pos(), pkg, name.Name, types.Typ[types.UntypedNil]))
						}
					}
				}
			case *ast.FuncDecl:
				if !d.Name.IsExported() || scope.Lookup(d.Name.Name) != nil {
					continue
				}
				sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
				scope.Insert(types.NewFunc(d.Name.Pos(), pkg, d.Name.Name, sig))
			}
		}
	}
	return pkg, nil
}

func parserErrorContext(err error, source string) string {
	var parseErr scanner.ErrorList
	if !errors.As(err, &parseErr) || len(parseErr) == 0 {
		return ""
	}
	lines := strings.Split(source, "\n")
	first := parseErr[0]
	line := first.Pos.Line
	start := line - 2
	if start < 1 {
		start = 1
	}
	end := line + 2
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	sb.WriteString("generated source context:\n")
	for i := start; i <= end; i++ {
		fmt.Fprintf(&sb, "%4d | %s\n", i, lines[i-1])
	}
	return sb.String()
}

func (g *Generator) resolveToBasicType(e ast.Expr) string {
	if g.typeInfo == nil || e == nil {
		return ""
	}
	if tv, ok := g.typeInfo.Types[e]; ok {
		underlying := tv.Type.Underlying()
		if basic, ok := underlying.(*types.Basic); ok {
			return basic.Name()
		}
	}
	return ""
}

func parseTargetMeta(doc *ast.CommentGroup) targetMeta {
	var meta targetMeta
	if doc == nil {
		return meta
	}
	for _, comment := range doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
		if !strings.HasPrefix(text, "ffigen:") {
			continue
		}
		switch {
		case strings.HasPrefix(text, "ffigen:module"):
			meta.moduleName = strings.TrimSpace(strings.TrimPrefix(text, "ffigen:module"))
		case strings.HasPrefix(text, "ffigen:methods"):
			meta.methodsMarked = true
			meta.methodsPrefix = strings.TrimSpace(strings.TrimPrefix(text, "ffigen:methods"))
		case strings.HasPrefix(text, "ffigen:interface"):
			meta.interfaceMarked = true
		}
	}
	return meta
}

func (g *Generator) derivePackageDefaultModule(files []*ast.File) string {
	return deriveModuleFromFiles(g.fset, files)
}

func materializeTargetMeta(meta targetMeta, defaultModule string) targetMeta {
	if meta.moduleName == "" && meta.methodsPrefix != "" {
		meta.moduleName = defaultModule
	}
	return meta
}

func (g *Generator) resolveImportedModule(importPath string) string {
	if importPath == "" {
		return ""
	}
	if g.moduleCache == nil {
		g.moduleCache = make(map[string]string)
	}
	if moduleName, ok := g.moduleCache[importPath]; ok {
		return moduleName
	}
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	out, err := cmd.Output()
	if err != nil {
		g.moduleCache[importPath] = ""
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		g.moduleCache[importPath] = ""
		return ""
	}
	tempSet := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		g.moduleCache[importPath] = ""
		return ""
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		file, parseErr := parser.ParseFile(tempSet, filePath, nil, parser.ParseComments)
		if parseErr == nil {
			if isGeneratedFile(entry.Name(), file) {
				continue
			}
			files = append(files, file)
		}
	}
	moduleName := deriveModuleFromFiles(tempSet, files)
	g.moduleCache[importPath] = moduleName
	return moduleName
}

func deriveModuleFromFiles(fileSet *token.FileSet, files []*ast.File) string {
	modules := make(map[string]bool)
	for _, file := range files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				doc := resolveTargetDocWithFileSet(fileSet, file, genDecl, typeSpec)
				meta := parseTargetMeta(doc)
				if meta.moduleName != "" {
					modules[meta.moduleName] = true
				}
			}
		}
	}
	if len(modules) != 1 {
		return ""
	}
	for moduleName := range modules {
		return moduleName
	}
	return ""
}
