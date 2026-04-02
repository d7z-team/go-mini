package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var (
	pkgName = flag.String("pkg", "", "package name")
	outFile = flag.String("out", "", "output file")

	// 类型推导上下文
	typeInfo     *types.Info
	fset         *token.FileSet
	knownImports map[string]string
	moduleCache  map[string]string
	packagePath  string
)

type targetMeta struct {
	moduleName    string
	methodsPrefix string
	methodsMarked bool
	reverse       bool
	structTarget  bool
}

type ffigenTarget struct {
	spec *ast.TypeSpec
	meta targetMeta
}

type displayTypeResolver struct {
	moduleName        string
	importAliases     map[string]string
	collidingBaseName map[string]bool
}

func main() {
	flag.Parse()
	if *pkgName == "" || *outFile == "" {
		fmt.Println("Usage: ffigen -pkg <name> -out <file> [input files...]")
		os.Exit(1)
	}

	if len(flag.Args()) == 0 {
		fmt.Println("Error: no input files provided")
		os.Exit(1)
	}

	{
		dir := filepath.Dir(flag.Args()[0])
		cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deriving import path in %s: %v\n", dir, err)
			os.Exit(1)
		}
		packagePath = strings.TrimSpace(string(out))
		if packagePath == "" || packagePath == "." {
			fmt.Fprintf(os.Stderr, "Error: derived import path is empty or invalid.\n")
			os.Exit(1)
		}
	}

	fset = token.NewFileSet()
	var allFiles []*ast.File
	seenDirs := make(map[string]bool)

	// Map to quickly find parsed files by their absolute path
	parsedFiles := make(map[string]*ast.File)

	absOutFile, _ := filepath.Abs(*outFile)

	for _, arg := range flag.Args() {
		absPath, err := filepath.Abs(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for %s: %v\n", arg, err)
			os.Exit(1)
		}

		dir := filepath.Dir(absPath)
		if !seenDirs[dir] {
			seenDirs[dir] = true
			entries, err := os.ReadDir(dir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading directory %s: %v\n", dir, err)
				os.Exit(1)
			}

			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					continue
				}

				filePath := filepath.Join(dir, entry.Name())
				absFilePath, _ := filepath.Abs(filePath)

				// Skip the output file to avoid circularity/conflicts
				if absFilePath == absOutFile {
					continue
				}

				f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error parsing file %s: %v\n", filePath, err)
					os.Exit(1)
				}

				allFiles = append(allFiles, f)
				parsedFiles[absFilePath] = f
			}
		}

		// Ensure the explicitly provided file is in parsedFiles even if it was skipped or is elsewhere
		if _, ok := parsedFiles[absPath]; !ok {
			f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing explicitly provided file %s: %v\n", absPath, err)
				os.Exit(1)
			}
			allFiles = append(allFiles, f)
			parsedFiles[absPath] = f
		}
	}

	// Reconstruct inputFiles in the order they were provided
	var inputFiles []*ast.File
	for _, arg := range flag.Args() {
		abs, _ := filepath.Abs(arg)
		if f, ok := parsedFiles[abs]; ok {
			inputFiles = append(inputFiles, f)
		}
	}

	// 执行类型检查以获取跨包的 Underlying 类型信息
	conf := types.Config{Importer: importer.Default()}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	typeInfo = info
	_, _ = conf.Check(*pkgName, fset, allFiles, info)

	packageDefaultModule := derivePackageDefaultModule(allFiles)
	var targets []ffigenTarget
	globalConsts := make(map[string]string)
	structs := make(map[string]*ast.StructType)
	knownImports = make(map[string]string)
	moduleCache = make(map[string]string)

	// Step 1: Collect package-wide information from allFiles
	for _, node := range allFiles {
		for _, imp := range node.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			var alias string
			if imp.Name != nil {
				alias = imp.Name.Name
			} else {
				parts := strings.Split(path, "/")
				alias = parts[len(parts)-1]
			}
			knownImports[alias] = path
		}

		ast.Inspect(node, func(n ast.Node) bool {
			if gd, ok := n.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if str, ok := typeSpec.Type.(*ast.StructType); ok {
							structs[typeSpec.Name.Name] = str
						}
					}
					if valSpec, ok := spec.(*ast.ValueSpec); ok {
						for i, name := range valSpec.Names {
							if !name.IsExported() || i >= len(valSpec.Values) {
								continue
							}
							val := exprToString(valSpec.Values[i])
							if val != "" {
								globalConsts[name.Name] = val
							}
						}
					}
				}
			}
			return true
		})
	}

	// Step 2: Collect ffigen targets from inputFiles in order
	for _, node := range inputFiles {
		ast.Inspect(node, func(n ast.Node) bool {
			if gd, ok := n.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						meta := parseTargetMeta(resolveTargetDoc(node, gd, typeSpec))

						if _, ok := typeSpec.Type.(*ast.InterfaceType); ok {
							targets = append(targets, ffigenTarget{
								spec: typeSpec,
								meta: materializeTargetMeta(meta, packageDefaultModule),
							})
						} else if _, ok := typeSpec.Type.(*ast.StructType); ok {
							if meta.moduleName != "" || meta.methodsMarked || meta.reverse {
								isModule := false
								if meta.moduleName != "" {
									isModule = true
								}
								methodsPrefix := meta.methodsPrefix
								if meta.methodsMarked && methodsPrefix == "" {
									methodsPrefix = typeSpec.Name.Name
								}

								methods := findMethodsForStruct(allFiles, typeSpec.Name.Name)
								if len(methods) > 0 {
									virtualIface := synthesizeInterface(methods, !isModule)
									virtualSpec := *typeSpec
									virtualSpec.Type = virtualIface
									targets = append(targets, ffigenTarget{
										spec: &virtualSpec,
										meta: materializeTargetMeta(targetMeta{
											moduleName:    meta.moduleName,
											methodsPrefix: methodsPrefix,
											methodsMarked: meta.methodsMarked,
											reverse:       meta.reverse,
											structTarget:  true,
										}, packageDefaultModule),
									})
								}
							}
						}
					}
				}
			}
			return true
		})
	}

	var interfaces []string
	for _, target := range targets {
		interfaces = append(interfaces, generateCode(*pkgName, target.spec, structs, target.meta, globalConsts))
	}

	fullInterfaces := strings.Join(interfaces, "\n")

	var sb strings.Builder
	sb.WriteString("// Code generated by ffigen. DO NOT EDIT.\n")
	fmt.Fprintf(&sb, "package %s\n\n", *pkgName)
	sb.WriteString("import (\n")

	// Standard packages potentially used
	stdPackages := map[string]string{
		"context": "context",
		"fmt":     "fmt",
		"strings": "strings",
		"ast":     "gopkg.d7z.net/go-mini/core/ast",
		"ffigo":   "gopkg.d7z.net/go-mini/core/ffigo",
		"runtime": "gopkg.d7z.net/go-mini/core/runtime",
	}

	// Write all candidate imports first
	for _, path := range stdPackages {
		fmt.Fprintf(&sb, "\t\"%s\"\n", path)
	}
	for alias, path := range knownImports {
		if _, ok := stdPackages[alias]; ok {
			continue
		}
		if alias == path[strings.LastIndex(path, "/")+1:] {
			fmt.Fprintf(&sb, "\t\"%s\"\n", path)
		} else {
			fmt.Fprintf(&sb, "\t%s \"%s\"\n", alias, path)
		}
	}
	sb.WriteString(")\n\n")
	sb.WriteString(fullInterfaces)

	// Now parse the whole thing and find actually used aliases
	source := sb.String()
	fsetOut := token.NewFileSet()
	fOut, err := parser.ParseFile(fsetOut, "", source, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing generated code for import cleanup: %v\n", err)
		// Fallback to writing as is
		_ = os.WriteFile(*outFile, []byte(source), 0o644)
		return
	}

	usedAliases := make(map[string]bool)
	ast.Inspect(fOut, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := node.X.(*ast.Ident); ok {
				usedAliases[id.Name] = true
			}
		case *ast.Field: // Handle parameter and result types
			if node.Type != nil {
				ast.Inspect(node.Type, func(tn ast.Node) bool {
					if se, ok := tn.(*ast.SelectorExpr); ok {
						if id, ok := se.X.(*ast.Ident); ok {
							usedAliases[id.Name] = true
						}
					}
					return true
				})
			}
		}
		return true
	})

	// Filter and sort imports into two groups
	var stdSpecs []ast.Spec
	var extSpecs []ast.Spec
	var otherDecls []ast.Decl

	for _, decl := range fOut.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				is := spec.(*ast.ImportSpec)
				path := strings.Trim(is.Path.Value, "\"")
				var alias string
				if is.Name != nil {
					alias = is.Name.Name
				} else {
					alias = path[strings.LastIndex(path, "/")+1:]
				}
				if usedAliases[alias] {
					// Reset positions to allow re-sorting
					if is.Name != nil {
						is.Name.NamePos = token.NoPos
					}
					is.Path.ValuePos = token.NoPos
					is.EndPos = token.NoPos

					if !strings.Contains(path, ".") {
						stdSpecs = append(stdSpecs, is)
					} else {
						extSpecs = append(extSpecs, is)
					}
				}
			}
		} else {
			otherDecls = append(otherDecls, decl)
		}
	}

	sortSpecs := func(specs []ast.Spec) {
		sort.Slice(specs, func(i, j int) bool {
			pathI := strings.Trim(specs[i].(*ast.ImportSpec).Path.Value, "\"")
			pathJ := strings.Trim(specs[j].(*ast.ImportSpec).Path.Value, "\"")
			return pathI < pathJ
		})
	}
	sortSpecs(stdSpecs)
	sortSpecs(extSpecs)

	// Reconstruct Decls: standard imports, then external imports, then others
	var newDecls []ast.Decl
	if len(stdSpecs) > 0 {
		newDecls = append(newDecls, &ast.GenDecl{
			Tok:    token.IMPORT,
			Specs:  stdSpecs,
			Lparen: 1, // dummy value to force parenthesis if more than one, but we'll use a specific style
		})
	}
	if len(extSpecs) > 0 {
		newDecls = append(newDecls, &ast.GenDecl{
			Tok:    token.IMPORT,
			Specs:  extSpecs,
			Lparen: 1,
		})
	}
	newDecls = append(newDecls, otherDecls...)
	fOut.Decls = newDecls

	// Format and write
	var finalBuf bytes.Buffer
	if err := format.Node(&finalBuf, fsetOut, fOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting generated code: %v\n", err)
		_ = os.WriteFile(*outFile, []byte(source), 0o644)
		return
	}

	if err := os.WriteFile(*outFile, finalBuf.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

func resolveToBasicType(e ast.Expr) string {
	if typeInfo == nil || e == nil {
		return ""
	}
	if tv, ok := typeInfo.Types[e]; ok {
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
		case text == "ffigen:reverse":
			meta.reverse = true
		}
	}
	return meta
}

func derivePackageDefaultModule(files []*ast.File) string {
	return deriveModuleFromFiles(fset, files)
}

func materializeTargetMeta(meta targetMeta, defaultModule string) targetMeta {
	if meta.moduleName == "" && meta.methodsPrefix != "" {
		meta.moduleName = defaultModule
	}
	return meta
}

func resolveImportedModule(importPath string) string {
	if importPath == "" {
		return ""
	}
	if moduleName, ok := moduleCache[importPath]; ok {
		return moduleName
	}
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	out, err := cmd.Output()
	if err != nil {
		moduleCache[importPath] = ""
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		moduleCache[importPath] = ""
		return ""
	}
	tempSet := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		moduleCache[importPath] = ""
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
			files = append(files, file)
		}
	}
	moduleName := deriveModuleFromFiles(tempSet, files)
	moduleCache[importPath] = moduleName
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

func resolveTargetDoc(file *ast.File, genDecl *ast.GenDecl, typeSpec *ast.TypeSpec) *ast.CommentGroup {
	return resolveTargetDocWithFileSet(fset, file, genDecl, typeSpec)
}

func resolveTargetDocWithFileSet(fileSet *token.FileSet, file *ast.File, genDecl *ast.GenDecl, typeSpec *ast.TypeSpec) *ast.CommentGroup {
	if typeSpec.Doc != nil {
		return typeSpec.Doc
	}
	if genDecl.Doc != nil {
		return genDecl.Doc
	}
	specLine := fileSet.Position(typeSpec.Pos()).Line
	var best *ast.CommentGroup
	bestLine := -1
	for _, group := range file.Comments {
		endLine := fileSet.Position(group.End()).Line
		if endLine >= specLine || specLine-endLine > 2 {
			continue
		}
		if endLine > bestLine {
			best = group
			bestLine = endLine
		}
	}
	return best
}

func newDisplayTypeResolver(moduleName string, iface *ast.InterfaceType, structs map[string]*ast.StructType, methodsPrefix string) *displayTypeResolver {
	resolver := &displayTypeResolver{
		moduleName:        moduleName,
		importAliases:     make(map[string]string, len(knownImports)),
		collidingBaseName: make(map[string]bool),
	}
	for alias, path := range knownImports {
		resolver.importAliases[alias] = path
	}
	nameOwners := make(map[string]string)
	record := func(typeName string) {
		for _, named := range collectNamedTypeRefs(typeName) {
			baseName := named
			if idx := strings.LastIndex(baseName, "."); idx >= 0 {
				baseName = baseName[idx+1:]
			}
			owner := named
			if idx := strings.Index(owner, "."); idx >= 0 {
				owner = owner[:idx]
			}
			if previous, ok := nameOwners[baseName]; ok && previous != owner {
				resolver.collidingBaseName[baseName] = true
				continue
			}
			nameOwners[baseName] = owner
		}
	}
	record(methodsPrefix)
	if iface != nil {
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			funcType := method.Type.(*ast.FuncType)
			if funcType.Params != nil {
				for _, param := range funcType.Params.List {
					record(typeToString(param.Type))
				}
			}
			if funcType.Results != nil {
				for _, result := range funcType.Results.List {
					record(typeToString(result.Type))
				}
			}
		}
	}
	for _, structName := range collectReferencedStructs(iface, structs) {
		fieldMap := make(map[string]string)
		getFields(structs, structName, fieldMap)
		for _, fieldType := range fieldMap {
			record(fieldType)
		}
	}
	return resolver
}

func collectNamedTypeRefs(typeName string) []string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil
	}
	if strings.HasPrefix(typeName, "Ptr<") && strings.HasSuffix(typeName, ">") {
		return collectNamedTypeRefs(typeName[4 : len(typeName)-1])
	}
	if strings.HasPrefix(typeName, "Array<") && strings.HasSuffix(typeName, ">") {
		return collectNamedTypeRefs(typeName[6 : len(typeName)-1])
	}
	if strings.HasPrefix(typeName, "Map<") && strings.HasSuffix(typeName, ">") {
		inner := typeName[4 : len(typeName)-1]
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) != 2 {
			return nil
		}
		return append(
			collectNamedTypeRefs(strings.TrimSpace(parts[0])),
			collectNamedTypeRefs(strings.TrimSpace(parts[1]))...,
		)
	}
	if strings.HasPrefix(typeName, "tuple(") && strings.HasSuffix(typeName, ")") {
		var refs []string
		for _, part := range strings.Split(typeName[6:len(typeName)-1], ",") {
			refs = append(refs, collectNamedTypeRefs(strings.TrimSpace(part))...)
		}
		return refs
	}
	if isPrimitive(typeName) || typeName == "error" || typeName == "any" || typeName == "interface{}" || typeName == "context.Context" || typeName == "Context" {
		return nil
	}
	return []string{typeName}
}

func (r *displayTypeResolver) NormalizeTypeString(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return "Any"
	}
	if strings.HasPrefix(typeName, "Ptr<") && strings.HasSuffix(typeName, ">") {
		return "Ptr<" + r.NormalizeTypeString(typeName[4:len(typeName)-1]) + ">"
	}
	if strings.HasPrefix(typeName, "Array<") && strings.HasSuffix(typeName, ">") {
		return "Array<" + r.NormalizeTypeString(typeName[6:len(typeName)-1]) + ">"
	}
	if strings.HasPrefix(typeName, "Map<") && strings.HasSuffix(typeName, ">") {
		inner := typeName[4 : len(typeName)-1]
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("Map<%s, %s>", r.NormalizeTypeString(strings.TrimSpace(parts[0])), r.NormalizeTypeString(strings.TrimSpace(parts[1])))
		}
	}
	if strings.HasPrefix(typeName, "tuple(") && strings.HasSuffix(typeName, ")") {
		var normalized []string
		for _, part := range strings.Split(typeName[6:len(typeName)-1], ",") {
			normalized = append(normalized, r.NormalizeTypeString(strings.TrimSpace(part)))
		}
		return "tuple(" + strings.Join(normalized, ", ") + ")"
	}
	switch typeName {
	case "string":
		return "String"
	case "bool":
		return "Bool"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "byte":
		return "Int64"
	case "float32", "float64":
		return "Float64"
	case "[]byte":
		return "TypeBytes"
	case "error":
		return "Error"
	case "any", "interface{}":
		return "Any"
	case "context.Context", "Context":
		return "Context"
	}
	return r.displayName(typeName)
}

func (r *displayTypeResolver) VMType(expr ast.Expr) string {
	if bt := resolveToBasicType(expr); bt != "" {
		switch {
		case strings.HasPrefix(bt, "int") || strings.HasPrefix(bt, "uint"):
			return "Int64"
		case strings.HasPrefix(bt, "float"):
			return "Float64"
		case bt == "string":
			return "String"
		case bt == "bool":
			return "Bool"
		}
	}
	switch t := expr.(type) {
	case *ast.ArrayType:
		if ident, ok := t.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
			return "TypeBytes"
		}
		return fmt.Sprintf("Array<%s>", r.VMType(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", r.VMType(t.Key), r.VMType(t.Value))
	case *ast.StarExpr:
		return fmt.Sprintf("Ptr<%s>", r.VMType(t.X))
	case *ast.Ellipsis:
		return fmt.Sprintf("Array<%s>", r.VMType(t.Elt))
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "Any"
		}
		return "Any"
	default:
		return r.NormalizeTypeString(typeToString(expr))
	}
}

func (r *displayTypeResolver) displayName(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return "Any"
	}
	if strings.Contains(typeName, ".") {
		parts := strings.SplitN(typeName, ".", 2)
		if len(parts) == 2 {
			if importPath, ok := knownImports[parts[0]]; ok {
				if moduleName := resolveImportedModule(importPath); moduleName != "" {
					return moduleName + "." + parts[1]
				}
				return typeName
			}
		}
		if r.moduleName == "" {
			return typeName
		}
		return r.moduleName + "." + typeName
	}
	if r.moduleName == "" {
		return typeName
	}
	return r.moduleName + "." + typeName
}

func generateCode(pkg string, spec *ast.TypeSpec, structs map[string]*ast.StructType, meta targetMeta, constants map[string]string) string {
	name := spec.Name.Name
	iface := spec.Type.(*ast.InterfaceType)

	var sb strings.Builder
	methodsPrefix := meta.methodsPrefix
	moduleName := meta.moduleName
	isReverse := meta.reverse
	isStruct := meta.structTarget
	isModule := moduleName != ""

	displayResolver := newDisplayTypeResolver(moduleName, iface, structs, methodsPrefix)
	displayTypeName := func(typeName string) string { return displayResolver.NormalizeTypeString(typeName) }
	vmType := func(expr ast.Expr) string { return displayResolver.VMType(expr) }
	funcSpec := func(funcType *ast.FuncType) string {
		var params []string
		if funcType.Params != nil {
			for i, p := range funcType.Params.List {
				pType := vmType(p.Type)
				if i == 0 && (pType == "context.Context" || pType == "Context") {
					continue
				}
				prefix := ""
				if _, ok := p.Type.(*ast.Ellipsis); ok {
					prefix = "..."
					if strings.HasPrefix(pType, "Array<") && strings.HasSuffix(pType, ">") {
						pType = pType[6 : len(pType)-1]
					}
				}
				if len(p.Names) == 0 {
					params = append(params, prefix+pType)
				} else {
					for range p.Names {
						params = append(params, prefix+pType)
					}
				}
			}
		}
		var results []string
		if funcType.Results != nil {
			for _, r := range funcType.Results.List {
				t := vmType(r.Type)
				if t == "error" {
					results = append(results, "Error")
				} else {
					results = append(results, t)
				}
			}
		}
		actualRet := "Void"
		if len(results) > 1 {
			actualRet = "tuple(" + strings.Join(results, ", ") + ")"
		} else if len(results) == 1 {
			actualRet = results[0]
		}
		return fmt.Sprintf("function(%s) %s", strings.Join(params, ", "), actualRet)
	}
	fixedPrefix := moduleName
	if methodsPrefix != "" {
		fixedPrefix = "__method_" + displayTypeName(methodsPrefix)
	}
	methodHasReceiver := func(funcType *ast.FuncType) bool {
		hasContext := false
		if funcType.Params != nil && len(funcType.Params.List) > 0 {
			pType := typeToString(funcType.Params.List[0].Type)
			if pType == "context.Context" || pType == "Context" {
				hasContext = true
			}
		}
		paramIdx := 0
		if hasContext {
			paramIdx = 1
		}
		if funcType.Params == nil || len(funcType.Params.List) <= paramIdx {
			return false
		}
		receiverType := typeToString(funcType.Params.List[paramIdx].Type)
		receiverType = strings.TrimPrefix(receiverType, "Ptr<")
		receiverType = strings.TrimSuffix(receiverType, ">")
		return receiverType == methodsPrefix
	}
	writeBoundRegistrations := func(indent string) {
		for i, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			methodName := method.Names[0].Name
			funcType := method.Type.(*ast.FuncType)
			routePrefix := fixedPrefix
			if moduleName != "" && methodsPrefix != "" && !methodHasReceiver(funcType) {
				routePrefix = moduleName
			}
			routeSep := "."
			if strings.HasPrefix(routePrefix, "__method_") {
				routeSep = "_"
			}
			fmt.Fprintf(&sb, "%sregistrar.RegisterFFISchema(\"%s%s%s\", bridge, %s_FFI_Schemas[%d].MethodID, %s_FFI_Schemas[%d].Sig, %s_FFI_Schemas[%d].Doc)\n",
				indent, routePrefix, routeSep, methodName, name, i, name, i, name, i)
		}
	}

	if methodsPrefix != "" && !isReverse {
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			funcType := method.Type.(*ast.FuncType)
			if methodHasReceiver(funcType) {
				continue
			}
			if moduleName != "" {
				continue
			}
			panic(fmt.Sprintf("ffigen:methods validation failed! Interface '%s' method '%s' must use receiver '%s' (or declare ffigen:module for module-level functions).", name, method.Names[0].Name, methodsPrefix))
		}
	}

	buildStructSchemaLiteral := func(structName string, includeFields bool, includeMethods bool) string {
		var fieldsSB strings.Builder
		fieldsSB.WriteString("struct { ")
		if includeFields {
			if str, ok := structs[structName]; ok {
				var keys []string
				fMap := make(map[string]string)
				getFields(structs, structName, fMap)
				for k := range fMap {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(&fieldsSB, "%s %s; ", k, displayTypeName(fMap[k]))
				}
				_ = str
			}
		}
		if includeMethods {
			for _, method := range iface.Methods.List {
				if len(method.Names) == 0 {
					continue
				}
				mName := method.Names[0].Name
				fmt.Fprintf(&fieldsSB, "%s %s; ", mName, funcSpec(method.Type.(*ast.FuncType)))
			}
		}
		fieldsSB.WriteString("}")
		return fieldsSB.String()
	}

	referencedStructs := collectReferencedStructs(iface, structs)

	fmt.Fprintf(&sb, "const (\n")
	for i, method := range iface.Methods.List {
		if len(method.Names) > 0 {
			fmt.Fprintf(&sb, "\tMethodID_%s_%s = %d\n", name, method.Names[0].Name, i+1)
		}
	}
	fmt.Fprintf(&sb, ")\n\n")

	if !isStruct {
		fmt.Fprintf(&sb, "type %sProxy struct {\n\tbridge ffigo.FFIBridge\n\tregistry *ffigo.HandleRegistry\n}\n\n", name)
		fmt.Fprintf(&sb, "func New%sProxy(bridge ffigo.FFIBridge, registry *ffigo.HandleRegistry) %s {\n\treturn &%sProxy{bridge: bridge, registry: registry}\n}\n\n", name, name, name)

		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			methodName := method.Names[0].Name
			funcType := method.Type.(*ast.FuncType)

			hasContext := false
			contextVarName := "context.Background()"
			if funcType.Params != nil && len(funcType.Params.List) > 0 {
				pType := typeToString(funcType.Params.List[0].Type)
				if pType == "context.Context" || pType == "Context" {
					hasContext = true
					if len(funcType.Params.List[0].Names) > 0 {
						contextVarName = funcType.Params.List[0].Names[0].Name
					} else {
						contextVarName = "arg0"
					}
				}
			}

			fmt.Fprintf(&sb, "func (__p *%sProxy) %s(", name, methodName)
			var pList []string
			argIdx := 0
			if funcType.Params != nil {
				for _, param := range funcType.Params.List {
					goType := toGoType(typeToString(param.Type))
					if _, ok := param.Type.(*ast.Ellipsis); ok {
						goType = "..." + strings.TrimPrefix(goType, "[]")
					}
					if len(param.Names) == 0 {
						pList = append(pList, fmt.Sprintf("arg%d %s", argIdx, goType))
						argIdx++
					} else {
						for _, pName := range param.Names {
							pList = append(pList, pName.Name+" "+goType)
							argIdx++
						}
					}
				}
			}
			fmt.Fprintf(&sb, "%s) ", strings.Join(pList, ", "))

			var hasErr bool
			if funcType.Results != nil {
				fmt.Fprintf(&sb, "(")
				for j, result := range funcType.Results.List {
					rType := typeToString(result.Type)
					if rType == "error" {
						hasErr = true
						fmt.Fprintf(&sb, "error")
					} else {
						fmt.Fprintf(&sb, "%s", toGoType(rType))
					}
					if j < len(funcType.Results.List)-1 {
						fmt.Fprintf(&sb, ", ")
					}
				}
				fmt.Fprintf(&sb, ") ")
			}

			fmt.Fprintf(&sb, "{\n\tbuf := ffigo.GetBuffer()\n\tdefer ffigo.ReleaseBuffer(buf)\n\n")
			argIdx = 0
			if funcType.Params != nil {
				for j, param := range funcType.Params.List {
					if j == 0 && hasContext {
						argIdx++
						continue
					}
					pType := typeToString(param.Type)
					if len(param.Names) == 0 {
						argName := fmt.Sprintf("arg%d", argIdx)
						if _, ok := param.Type.(*ast.Ellipsis); ok {
							itemType, _ := readArrayItemType(pType)
							fmt.Fprintf(&sb, "\tbuf.WriteUvarint(uint64(len(%s)))\n", argName)
							fmt.Fprintf(&sb, "\tfor _, item := range %s {\n", argName)
							emitWrite(&sb, "item", itemType, param.Type.(*ast.Ellipsis).Elt, structs, "buf", false)
							fmt.Fprintf(&sb, "\t}\n")
						} else {
							emitWrite(&sb, argName, pType, param.Type, structs, "buf", false)
						}
						argIdx++
					} else {
						for _, pName := range param.Names {
							if _, ok := param.Type.(*ast.Ellipsis); ok {
								itemType, _ := readArrayItemType(pType)
								fmt.Fprintf(&sb, "\tbuf.WriteUvarint(uint64(len(%s)))\n", pName.Name)
								fmt.Fprintf(&sb, "\tfor _, item := range %s {\n", pName.Name)
								emitWrite(&sb, "item", itemType, param.Type.(*ast.Ellipsis).Elt, structs, "buf", false)
								fmt.Fprintf(&sb, "\t}\n")
							} else {
								emitWrite(&sb, pName.Name, pType, param.Type, structs, "buf", false)
							}
							argIdx++
						}
					}
				}
			}

			needsRetBuf := funcType.Results != nil && len(funcType.Results.List) > 0
			if needsRetBuf || hasErr {
				fmt.Fprintf(&sb, "\n\tretData, err := __p.bridge.Call(%s, MethodID_%s_%s, buf.Bytes())\n", contextVarName, name, methodName)
				fmt.Fprintf(&sb, "\t_ = retData\n")
			} else {
				fmt.Fprintf(&sb, "\n\t_, err := __p.bridge.Call(%s, MethodID_%s_%s, buf.Bytes())\n", contextVarName, name, methodName)
			}
			fmt.Fprintf(&sb, "\t_ = err\n")

			if hasErr {
				fmt.Fprintf(&sb, "\tif err != nil { return ")
				if funcType.Results != nil {
					for j, result := range funcType.Results.List {
						rType := typeToString(result.Type)
						if rType == "error" {
							fmt.Fprintf(&sb, "err")
						} else {
							fmt.Fprintf(&sb, "%s", zeroValue(toGoType(rType)))
						}
						if j < len(funcType.Results.List)-1 {
							fmt.Fprintf(&sb, ", ")
						}
					}
				}
				fmt.Fprintf(&sb, " }\n")
			}

			if needsRetBuf {
				fmt.Fprintf(&sb, "\tretBuf := ffigo.NewReader(retData)\n")
			}

			var retStmt []string
			if funcType.Results != nil {
				for i, result := range funcType.Results.List {
					rType := typeToString(result.Type)
					if rType == "error" {
						fmt.Fprintf(&sb, "\tvar err_%d error\n", i)
						fmt.Fprintf(&sb, "\tif retBuf.Available() > 0 {\n")
						fmt.Fprintf(&sb, "\t\ted := retBuf.ReadRawError()\n")
						fmt.Fprintf(&sb, "\t\tif ed.Message != \"\" || ed.Handle != 0 {\n")
						fmt.Fprintf(&sb, "\t\t\tif ed.Handle != 0 && __p.registry != nil {\n")
						fmt.Fprintf(&sb, "\t\t\t\tif obj, ok := __p.registry.Get(ed.Handle); ok { err_%d = obj.(error) } else { err_%d = ed }\n", i, i)
						fmt.Fprintf(&sb, "\t\t\t} else { err_%d = ed }\n", i)
						fmt.Fprintf(&sb, "\t\t}\n\t}\n")
						retStmt = append(retStmt, fmt.Sprintf("err_%d", i))
						continue
					}
					varName := fmt.Sprintf("v_%d", i)
					fmt.Fprintf(&sb, "\tvar %s %s\n", varName, toGoType(rType))
					emitReadAssign(&sb, varName, rType, result.Type, structs, "retBuf", false)
					retStmt = append(retStmt, varName)
				}
			}
			if len(retStmt) > 0 {
				fmt.Fprintf(&sb, "\treturn %s\n", strings.Join(retStmt, ", "))
			} else {
				fmt.Fprintf(&sb, "\treturn\n")
			}
			fmt.Fprintf(&sb, "}\n\n")
		}
	}

	implType := name
	if isStruct {
		implType = "*" + name
	}
	fmt.Fprintf(&sb, "func %sHostRouter(ctx context.Context, impl %s, registry *ffigo.HandleRegistry, methodID uint32, methodName string, args []byte) (retData []byte, bridgeErr error) {\n", name, implType)
	fmt.Fprintf(&sb, "\tif methodID == 0 && methodName != \"\" {\n")
	fmt.Fprintf(&sb, "\t\tswitch methodName {\n")
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		methodName := method.Names[0].Name
		fmt.Fprintf(&sb, "\t\tcase \"%s\":\n", methodName)
		fmt.Fprintf(&sb, "\t\t\tmethodID = MethodID_%s_%s\n", name, methodName)
	}
	fmt.Fprintf(&sb, "\t\t}\n")
	fmt.Fprintf(&sb, "\t}\n\n")

	needsReqBuf := false
	needsRawVal := false
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		funcType := method.Type.(*ast.FuncType)
		hasContext := false
		if funcType.Params != nil && len(funcType.Params.List) > 0 {
			pType := typeToString(funcType.Params.List[0].Type)
			if pType == "context.Context" || pType == "Context" {
				hasContext = true
			}
		}
		if funcType.Params != nil {
			for j, param := range funcType.Params.List {
				if j == 0 && hasContext {
					continue
				}
				needsReqBuf = true
				pType := typeToString(param.Type)
				if pType == "Any" || pType == "any" || strings.Contains(pType, "<Any>") || strings.Contains(pType, "<any>") {
					needsRawVal = true
				}
				if _, ok := param.Type.(*ast.Ellipsis); ok {
					// Also check variadic element type
					inner := typeToString(param.Type.(*ast.Ellipsis).Elt)
					if inner == "Any" || inner == "any" {
						needsRawVal = true
					}
				}
			}
		}
	}

	if needsReqBuf {
		fmt.Fprintf(&sb, "\treqBuf := ffigo.NewReader(args)\n")
	}
	if needsRawVal {
		fmt.Fprintf(&sb, "\tvar rawVal any\n\t_ = rawVal\n")
	}
	fmt.Fprintf(&sb, "\tswitch methodID {\n")
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		methodName := method.Names[0].Name
		funcType := method.Type.(*ast.FuncType)
		hasContext := false
		if funcType.Params != nil && len(funcType.Params.List) > 0 {
			pType := typeToString(funcType.Params.List[0].Type)
			if pType == "context.Context" || pType == "Context" {
				hasContext = true
			}
		}

		fmt.Fprintf(&sb, "\tcase MethodID_%s_%s:\n", name, methodName)
		var paramVars []string
		argIdx := 0
		if hasContext {
			paramVars = append(paramVars, "ctx")
			argIdx++
		}
		if funcType.Params != nil {
			for j, param := range funcType.Params.List {
				if j == 0 && hasContext {
					continue
				}
				pType := typeToString(param.Type)
				goType := toGoType(pType)
				isVariadic := false
				if _, ok := param.Type.(*ast.Ellipsis); ok {
					isVariadic = true
					goType = "[]" + strings.TrimPrefix(goType, "[]")
				}
				if len(param.Names) == 0 {
					argName := fmt.Sprintf("arg%d", argIdx)
					fmt.Fprintf(&sb, "\t\tvar %s %s\n", argName, goType)
					emitReadAssign(&sb, argName, pType, param.Type, structs, "reqBuf", true)
					if isVariadic {
						paramVars = append(paramVars, argName+"...")
					} else {
						paramVars = append(paramVars, argName)
					}
					argIdx++
				} else {
					for _, pName := range param.Names {
						fmt.Fprintf(&sb, "\t\tvar %s %s\n", pName.Name, goType)
						emitReadAssign(&sb, pName.Name, pType, param.Type, structs, "reqBuf", true)
						if isVariadic {
							paramVars = append(paramVars, pName.Name+"...")
						} else {
							paramVars = append(paramVars, pName.Name)
						}
						argIdx++
					}
				}
			}
		}

		callPrefix := "impl."
		callParams := paramVars
		if isStruct && methodsPrefix != "" {
			paramIdx := 0
			if hasContext {
				paramIdx = 1
			}
			if len(paramVars) > paramIdx {
				receiverVar := paramVars[paramIdx]
				callPrefix = receiverVar + "."
				// Remove receiver from callParams
				newCallParams := append([]string{}, paramVars[:paramIdx]...)
				newCallParams = append(newCallParams, paramVars[paramIdx+1:]...)
				callParams = newCallParams
			}
		}

		var retVars []string
		if funcType.Results != nil {
			for i, result := range funcType.Results.List {
				rName := fmt.Sprintf("r%d", i)
				if typeToString(result.Type) == "error" {
					rName = "err"
				}
				retVars = append(retVars, rName)
			}
			if len(retVars) > 0 {
				fmt.Fprintf(&sb, "\t\t%s := %s%s(%s)\n", strings.Join(retVars, ", "), callPrefix, methodName, strings.Join(callParams, ", "))
			} else {
				fmt.Fprintf(&sb, "\t\t%s%s(%s)\n", callPrefix, methodName, strings.Join(callParams, ", "))
			}
		} else {
			fmt.Fprintf(&sb, "\t\t%s%s(%s)\n", callPrefix, methodName, strings.Join(callParams, ", "))
		}
		fmt.Fprintf(&sb, "\t\tresBuf := ffigo.GetBuffer()\n")
		if funcType.Results != nil {
			for i, result := range funcType.Results.List {
				if typeToString(result.Type) == "error" {
					fmt.Fprintf(&sb, "\t\tif err != nil {\n")
					fmt.Fprintf(&sb, "\t\t\tif registry != nil {\n\t\t\t\tresBuf.WriteRawError(err.Error(), registry.Register(err))\n\t\t\t} else {\n\t\t\t\tresBuf.WriteRawError(err.Error(), 0)\n\t\t\t}\n")
					fmt.Fprintf(&sb, "\t\t} else {\n\t\t\tresBuf.WriteRawError(\"\", 0)\n\t\t}\n")
				} else {
					emitWrite(&sb, fmt.Sprintf("r%d", i), typeToString(result.Type), result.Type, structs, "resBuf", true)
				}
			}
		}
		fmt.Fprintf(&sb, "\t\treturn resBuf.Bytes(), nil\n")
	}
	fmt.Fprintf(&sb, "\tdefault:\n\t\treturn nil, fmt.Errorf(\"unknown method ID %%d\", methodID)\n\t}\n}\n")

	fmt.Fprintf(&sb, "var %s_FFI_Schemas = []struct {\n\tName     string\n\tMethodID uint32\n\tSig      *runtime.RuntimeFuncSig\n\tDoc      string\n}{\n", name)
	for i, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		methodName := method.Names[0].Name
		doc := ""
		if method.Doc != nil {
			doc = strings.ReplaceAll(method.Doc.Text(), "\"", "\\\"")
			doc = strings.ReplaceAll(doc, "\n", " ")
			doc = strings.TrimSpace(doc)
		}
		fmt.Fprintf(&sb, "\t{\"%s\", %d, runtime.MustParseRuntimeFuncSig(ast.GoMiniType(\"%s\")), \"%s\"},\n", methodName, i+1, funcSpec(method.Type.(*ast.FuncType)), doc)
	}
	fmt.Fprintf(&sb, "}\n\n")

	fmt.Fprintf(&sb, "type %s_Bridge struct {\n\tImpl %s\n\tRegistry *ffigo.HandleRegistry\n}\n\n", name, implType)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {\n", name)
	fmt.Fprintf(&sb, "\treturn %sHostRouter(ctx, b.Impl, b.Registry, methodID, \"\", args)\n}\n\n", name)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {\n", name)
	fmt.Fprintf(&sb, "\treturn %sHostRouter(ctx, b.Impl, b.Registry, 0, method, args)\n}\n\n", name)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) DestroyHandle(handle uint32) error {\n\tif b.Registry != nil { b.Registry.Remove(handle) }\n\treturn nil\n}\n\n", name)

	for _, structName := range referencedStructs {
		if isStruct && structName == name {
			continue
		}
		fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(\"%s\", ast.GoMiniType(\"%s\"))\n\n",
			structSchemaVarName(structName),
			displayTypeName(structName),
			buildStructSchemaLiteral(structName, true, false),
		)
	}

	if isStruct && methodsPrefix != "" {
		// Method Set registration for STRUCT: NO 'impl' parameter
		fmt.Fprintf(&sb, "var %s_StructSchema = runtime.MustParseRuntimeStructSpec(\"%s\", ast.GoMiniType(\"%s\"))\n\n", name, displayTypeName(name), buildStructSchemaLiteral(name, true, true))
		fmt.Fprintf(&sb, "func Register%s(executor interface{ RegisterConstant(string, string) }, registry *ffigo.HandleRegistry) {\n", name)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: nil, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tregistrar, ok := executor.(interface{ RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string); RegisterStructSchema(string, *runtime.RuntimeStructSpec) })\n")
		fmt.Fprintf(&sb, "\tif !ok { panic(\"ffigen: executor does not support schema FFI registration\") }\n")
		writeBoundRegistrations("\t")
		for _, structName := range referencedStructs {
			if structName == name {
				continue
			}
			fmt.Fprintf(&sb, "\tregistrar.RegisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), structSchemaVarName(structName))
		}
		fmt.Fprintf(&sb, "\tregistrar.RegisterStructSchema(\"%s\", %s_StructSchema)\n", displayTypeName(name), name)
		fmt.Fprintf(&sb, "}\n")
	} else if isModule || methodsPrefix != "" {
		// Module or Interface-based Methods: REQUIRES 'impl'
		if methodsPrefix != "" {
			fmt.Fprintf(&sb, "var %s_StructSchema = runtime.MustParseRuntimeStructSpec(\"%s\", ast.GoMiniType(\"%s\"))\n\n", name, displayTypeName(methodsPrefix), buildStructSchemaLiteral("", false, true))
		}
		fmt.Fprintf(&sb, "func Register%s(executor interface{ RegisterConstant(string, string) }, impl %s, registry *ffigo.HandleRegistry) {\n", name, implType)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: impl, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tregistrar, ok := executor.(interface{ RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string); RegisterStructSchema(string, *runtime.RuntimeStructSpec) })\n")
		fmt.Fprintf(&sb, "\tif !ok { panic(\"ffigen: executor does not support schema FFI registration\") }\n")
		writeBoundRegistrations("\t")

		if isModule && fixedPrefix != "" && len(constants) > 0 {
			var keys []string
			for k := range constants {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&sb, "\texecutor.RegisterConstant(\"%s.%s\", ffigo.ToConstantString(%s))\n", fixedPrefix, k, constants[k])
			}
		}

		if methodsPrefix != "" {
			fmt.Fprintf(&sb, "\t")
			for _, structName := range referencedStructs {
				if structName == methodsPrefix {
					continue
				}
				fmt.Fprintf(&sb, "registrar.RegisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), structSchemaVarName(structName))
			}
			fmt.Fprintf(&sb, "\tregistrar.RegisterStructSchema(\"%s\", %s_StructSchema)\n", displayTypeName(methodsPrefix), name)
		} else if len(referencedStructs) > 0 {
			for _, structName := range referencedStructs {
				fmt.Fprintf(&sb, "\tregistrar.RegisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), structSchemaVarName(structName))
			}
		}
		fmt.Fprintf(&sb, "}\n")
	} else {
		// Generic Library registration: Requires 'impl' and explicit prefix
		fmt.Fprintf(&sb, "func Register%sLibrary(executor interface{ RegisterConstant(string, string) }, prefix string, impl %s, registry *ffigo.HandleRegistry) {\n", name, implType)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: impl, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tregistrar, ok := executor.(interface{ RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string); RegisterStructSchema(string, *runtime.RuntimeStructSpec) })\n")
		fmt.Fprintf(&sb, "\tif !ok { panic(\"ffigen: executor does not support schema FFI registration\") }\n")
		fmt.Fprintf(&sb, "\tsep := \".\"\n\tif strings.HasPrefix(prefix, \"__method_\") { sep = \"_\" }\n")
		fmt.Fprintf(&sb, "\tfor _, m := range %s_FFI_Schemas {\n\t\tregistrar.RegisterFFISchema(prefix+sep+m.Name, bridge, m.MethodID, m.Sig, m.Doc)\n\t}\n", name)
		for _, structName := range referencedStructs {
			fmt.Fprintf(&sb, "\tregistrar.RegisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), structSchemaVarName(structName))
		}
		fmt.Fprintf(&sb, "}\n")
	}

	if isReverse && !isStruct {
		fmt.Fprintf(&sb, "type %s_ReverseProxy struct {\n\tprogram runtime.ExecutorAPI\n\tctx *runtime.StackContext\n\tcallable *runtime.Var\n\tbridge ffigo.FFIBridge\n}\n\n", name)
		fmt.Fprintf(&sb, "func New%s_ReverseProxy(program runtime.ExecutorAPI, ctx *runtime.StackContext, callable *runtime.Var, bridge ffigo.FFIBridge) *%s_ReverseProxy {\n\treturn &%s_ReverseProxy{program: program, ctx: ctx, callable: callable, bridge: bridge}\n}\n\n", name, name, name)
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			mName := method.Names[0].Name
			fType := method.Type.(*ast.FuncType)
			var pList []string
			var pNames []string
			argIdx := 0
			if fType.Params != nil {
				for j, p := range fType.Params.List {
					gType := toGoType(typeToString(p.Type))
					if j == 0 && (gType == "context.Context" || gType == "Context") {
						pList = append(pList, "ctx "+gType)
						argIdx++
						continue
					}
					if len(p.Names) == 0 {
						argName := fmt.Sprintf("arg%d", argIdx)
						pList = append(pList, argName+" "+gType)
						pNames = append(pNames, argName)
						argIdx++
					} else {
						for _, pn := range p.Names {
							pList = append(pList, pn.Name+" "+gType)
							pNames = append(pNames, pn.Name)
							argIdx++
						}
					}
				}
			}
			retT := " "
			if fType.Results != nil {
				var res []string
				for _, r := range fType.Results.List {
					res = append(res, toGoType(typeToString(r.Type)))
				}
				if len(res) > 1 {
					retT = " (" + strings.Join(res, ", ") + ") "
				} else if len(res) == 1 {
					retT = " " + res[0] + " "
				}
			}
			fmt.Fprintf(&sb, "func (__p *%s_ReverseProxy) %s(%s)%s{\n", name, mName, strings.Join(pList, ", "), retT)
			fmt.Fprintf(&sb, "\targs := make([]*runtime.Var, %d)\n", len(pNames))
			for i, pn := range pNames {
				fmt.Fprintf(&sb, "\targs[%d] = __p.program.ToVar(__p.ctx, %s, __p.bridge)\n", i, pn)
			}
			fmt.Fprintf(&sb, "\tresVar, err := __p.program.InvokeCallable(__p.ctx, __p.callable, \"%s\", args)\n\t_ = resVar\n", mName)
			if fType.Results != nil {
				var rStmts []string
				resultTypes := make([]string, 0, len(fType.Results.List))
				hasErrorReturn := false
				errorIndex := -1
				for i, r := range fType.Results.List {
					rt := typeToString(r.Type)
					resultTypes = append(resultTypes, rt)
					if rt == "error" {
						hasErrorReturn = true
						errorIndex = i
					}
				}
				if hasErrorReturn {
					zeroReturns := make([]string, 0, len(resultTypes))
					for i, rt := range resultTypes {
						if i == errorIndex {
							zeroReturns = append(zeroReturns, "err")
							continue
						}
						zeroReturns = append(zeroReturns, zeroValue(toGoType(rt)))
					}
					fmt.Fprintf(&sb, "\tif err != nil { return %s }\n", strings.Join(zeroReturns, ", "))
				} else {
					fmt.Fprintf(&sb, "\t_ = err\n")
				}
				if len(fType.Results.List) > 1 {
					fmt.Fprintf(&sb, "\tvar elements []*runtime.Var\n\tif resVar != nil && resVar.VType == runtime.TypeArray { if arr, ok := resVar.Ref.(*runtime.VMArray); ok { elements = arr.Data } }\n")
					for i, r := range fType.Results.List {
						rt := typeToString(r.Type)
						gt := toGoType(rt)
						vn := fmt.Sprintf("ret%d", i)
						fmt.Fprintf(&sb, "\tvar %s %s = %s\n\tif %d < len(elements) {", vn, gt, zeroValue(gt), i)
						emitReverseRead(&sb, vn, rt, fmt.Sprintf("elements[%d]", i))
						fmt.Fprintf(&sb, "}\n")
						rStmts = append(rStmts, vn)
					}
				} else {
					rt := typeToString(fType.Results.List[0].Type)
					gt := toGoType(rt)
					fmt.Fprintf(&sb, "\tvar ret0 %s = %s\n", gt, zeroValue(gt))
					emitReverseRead(&sb, "ret0", rt, "resVar")
					rStmts = append(rStmts, "ret0")
				}
				fmt.Fprintf(&sb, "\treturn %s\n", strings.Join(rStmts, ", "))
			} else {
				fmt.Fprintf(&sb, "\t_ = err\n")
				fmt.Fprintf(&sb, "\treturn\n")
			}
			fmt.Fprintf(&sb, "}\n\n")
		}
	}

	return sb.String()
}

func structSchemaVarName(typeName string) string {
	replacer := strings.NewReplacer("/", "_", ".", "_", "<", "_", ">", "", ",", "_", " ", "_", "*", "_")
	return replacer.Replace(typeName) + "_FFI_StructSchema"
}

func collectReferencedStructs(iface *ast.InterfaceType, structs map[string]*ast.StructType) []string {
	seen := make(map[string]bool)
	var ordered []string
	var visitType func(string)
	visitType = func(typeName string) {
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			return
		}
		if strings.HasPrefix(typeName, "Ptr<") && strings.HasSuffix(typeName, ">") {
			visitType(typeName[4 : len(typeName)-1])
			return
		}
		if strings.HasPrefix(typeName, "Array<") && strings.HasSuffix(typeName, ">") {
			visitType(typeName[6 : len(typeName)-1])
			return
		}
		if strings.HasPrefix(typeName, "Map<") && strings.HasSuffix(typeName, ">") {
			inner := typeName[4 : len(typeName)-1]
			parts := strings.SplitN(inner, ",", 2)
			if len(parts) == 2 {
				visitType(strings.TrimSpace(parts[0]))
				visitType(strings.TrimSpace(parts[1]))
			}
			return
		}
		if strings.HasPrefix(typeName, "tuple(") && strings.HasSuffix(typeName, ")") {
			inner := typeName[6 : len(typeName)-1]
			for _, part := range strings.Split(inner, ",") {
				visitType(strings.TrimSpace(part))
			}
			return
		}
		if isPrimitive(typeName) || strings.HasPrefix(typeName, "interface{") {
			return
		}
		localName := typeName
		if idx := strings.LastIndex(localName, "."); idx >= 0 {
			localName = localName[idx+1:]
		}
		if !seen[localName] && structs[localName] != nil {
			seen[localName] = true
			ordered = append(ordered, localName)
			fieldMap := make(map[string]string)
			getFields(structs, localName, fieldMap)
			for _, fieldType := range fieldMap {
				visitType(fieldType)
			}
		}
	}
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		funcType := method.Type.(*ast.FuncType)
		if funcType.Params != nil {
			for _, param := range funcType.Params.List {
				visitType(typeToString(param.Type))
			}
		}
		if funcType.Results != nil {
			for _, result := range funcType.Results.List {
				visitType(typeToString(result.Type))
			}
		}
	}
	return ordered
}

func emitWrite(sb *strings.Builder, prefix, pType string, expr ast.Expr, structs map[string]*ast.StructType, bufName string, isHost bool) {
	if strings.HasPrefix(pType, "Ptr<") {
		fmt.Fprintf(sb, "\t// Ptr<T> crosses the FFI boundary as an opaque handle ID.\n")
		fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteUvarint(0)\n\t} else {\n", prefix, bufName)
		if isHost {
			fmt.Fprintf(sb, "\t\t%s.WriteUvarint(uint64(registry.Register(%s)))\n", bufName, prefix)
		} else {
			fmt.Fprintf(sb, "\t\tif __p.registry != nil { %s.WriteUvarint(uint64(__p.registry.Register(%s))) } else { %s.WriteUvarint(0) }\n", bufName, prefix, bufName)
		}

		fmt.Fprintf(sb, "\t}\n")
		return
	}
	if strings.HasPrefix(pType, "interface{") {
		fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteRawInterface(0, nil)\n\t} else {\n\t\tmethods := make(map[string]string)\n", prefix, bufName)
		if isHost {
			fmt.Fprintf(sb, "\t\t%s.WriteRawInterface(registry.Register(%s), methods)\n", bufName, prefix)
		} else {
			fmt.Fprintf(sb, "\t\tif __p.registry != nil { %s.WriteRawInterface(__p.registry.Register(%s), methods) } else { %s.WriteRawInterface(0, nil) }\n", bufName, prefix, bufName)
		}
		fmt.Fprintf(sb, "\t}\n")
		return
	}

	bt := resolveToBasicType(expr)
	if bt == "" {
		switch pType {
		case "int", "int8", "int16", "int32", "int64", "Int", "Int8", "Int16", "Int32", "Int64":
			bt = "int64"
		case "uint", "uint8", "uint16", "uint32", "uint64", "Uint", "Uint8", "Uint16", "Uint32", "Uint64", "byte":
			bt = "uint64"
		case "float32", "float64", "Float32", "Float64":
			bt = "float64"
		case "string", "String":
			bt = "string"
		case "bool", "Bool":
			bt = "bool"
		}
	}

	if bt != "" {
		switch {
		case strings.HasPrefix(bt, "int"):
			fmt.Fprintf(sb, "\t%s.WriteVarint(int64(%s))\n", bufName, prefix)
			return
		case strings.HasPrefix(bt, "uint") || bt == "byte":
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(%s))\n", bufName, prefix)
			return
		case strings.HasPrefix(bt, "float"):
			fmt.Fprintf(sb, "\t%s.WriteFloat64(float64(%s))\n", bufName, prefix)
			return
		case bt == "string":
			fmt.Fprintf(sb, "\t%s.WriteString(string(%s))\n", bufName, prefix)
			return
		case bt == "bool":
			fmt.Fprintf(sb, "\t%s.WriteBool(bool(%s))\n", bufName, prefix)
			return
		}
	}

	switch pType {
	case "[]byte", "TypeBytes", "Array<Uint8>", "Array<byte>":
		fmt.Fprintf(sb, "\t%s.WriteBytes(%s)\n", bufName, prefix)
	case "Any", "any":
		fmt.Fprintf(sb, "\t%s.WriteAny(%s)\n", bufName, prefix)
	default:
		if itemType, ok := readArrayItemType(pType); ok {
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(len(%s)))\n\tfor _, item := range %s {\n", bufName, prefix, prefix)
			emitWrite(sb, "item", itemType, nil, structs, bufName, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(pType); ok {
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(len(%s)))\n\tfor k, v := range %s {\n", bufName, prefix, prefix)
			emitWrite(sb, "k", kType, nil, structs, bufName, isHost)
			emitWrite(sb, "v", vType, nil, structs, bufName, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if _, ok := structs[pType]; ok {
			fMap := make(map[string]string)
			getFields(structs, pType, fMap)
			var fNames []string
			for fn := range fMap {
				fNames = append(fNames, fn)
			}
			sort.Strings(fNames)
			for _, fn := range fNames {
				emitWrite(sb, prefix+"."+fn, fMap[fn], nil, structs, bufName, isHost)
			}
		} else {
			fmt.Fprintf(sb, "\t// Treating %s as Handle\n", pType)
			gt := toGoType(pType)
			isN := strings.HasPrefix(gt, "*") || strings.HasPrefix(gt, "[]") || strings.HasPrefix(gt, "map[") || gt == "any" || gt == "error"
			if isN {
				fmt.Fprintf(sb, "\tif %s == nil { %s.WriteUvarint(0) } else {\n", prefix, bufName)
			}
			if isHost {
				fmt.Fprintf(sb, "\t\t%s.WriteUvarint(uint64(registry.Register(%s)))\n", bufName, prefix)
			} else {
				fmt.Fprintf(sb, "\t\tif __p.registry != nil { %s.WriteUvarint(uint64(__p.registry.Register(%s))) } else { %s.WriteUvarint(0) }\n", bufName, prefix, bufName)
			}
			if isN {
				fmt.Fprintf(sb, "\t}\n")
			}
		}
	}
}

func emitReadAssign(sb *strings.Builder, varName, pType string, expr ast.Expr, structs map[string]*ast.StructType, readerName string, isHost bool) {
	if strings.HasPrefix(pType, "Ptr<") {
		fmt.Fprintf(sb, "\t// Ptr<T> is restored from the opaque handle ID written on the FFI wire.\n")
		if isHost {
			fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if obj, err := registry.GetWithAudit(id); err == nil { %s = obj.(%s) } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) } }\n", readerName, varName, toGoType(pType), varName)
		} else {
			fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if __p.registry != nil { if obj, ok := __p.registry.Get(id); ok { %s = obj.(%s) } } }\n", readerName, varName, toGoType(pType))
		}
		return
	}
	if strings.HasPrefix(pType, "interface{") {
		fmt.Fprintf(sb, "\tif idat := %s.ReadRawInterface(); idat.Handle != 0 { _ = idat }\n", readerName)
		return
	}

	bt := resolveToBasicType(expr)
	if bt == "" {
		switch pType {
		case "int", "int8", "int16", "int32", "int64", "Int", "Int8", "Int16", "Int32", "Int64":
			bt = pType
		case "uint", "uint8", "uint16", "uint32", "uint64", "Uint", "Uint8", "Uint16", "Uint32", "Uint64", "byte":
			bt = pType
		case "float32", "float64", "Float32", "Float64":
			bt = pType
		case "string", "String":
			bt = "string"
		case "bool", "Bool":
			bt = "bool"
		}
	}

	if bt != "" {
		gt := toGoType(pType)
		switch {
		case strings.HasPrefix(bt, "int") || strings.HasPrefix(bt, "uint") || bt == "byte":
			fmt.Fprintf(sb, "\t{\n\ttmp := %s.ReadVarint()\n", readerName)
			switch bt {
			case "int8":
				fmt.Fprintf(sb, "\tif tmp < -128 || tmp > 127 { panic(fmt.Sprintf(\"ffi: int8 overflow: %%d\", tmp)) }\n")
			case "int16":
				fmt.Fprintf(sb, "\tif tmp < -32768 || tmp > 32767 { panic(fmt.Sprintf(\"ffi: int16 overflow: %%d\", tmp)) }\n")
			case "int32":
				fmt.Fprintf(sb, "\tif tmp < -2147483648 || tmp > 2147483647 { panic(fmt.Sprintf(\"ffi: int32 overflow: %%d\", tmp)) }\n")
			case "uint8", "byte":
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > 255 { panic(fmt.Sprintf(\"ffi: uint8 overflow: %%d\", tmp)) }\n")
			case "uint16":
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > 65535 { panic(fmt.Sprintf(\"ffi: uint16 overflow: %%d\", tmp)) }\n")
			case "uint32":
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > 4294967295 { panic(fmt.Sprintf(\"ffi: uint32 overflow: %%d\", tmp)) }\n")
			case "uint", "uint64":
				fmt.Fprintf(sb, "\tif tmp < 0 { panic(fmt.Sprintf(\"ffi: uint overflow: %%d\", tmp)) }\n")
			case "int":
				// Depending on the host architecture (32/64 bit), but we assume 64-bit for safe limits in VM
				// Go's int is at least 32 bits.
			}
			fmt.Fprintf(sb, "\t%s = %s(tmp)\n\t}\n", varName, gt)
			return
		case strings.HasPrefix(bt, "float"):
			fmt.Fprintf(sb, "\t%s = %s(%s.ReadFloat64())\n", varName, gt, readerName)
			return
		case bt == "string":
			fmt.Fprintf(sb, "\t%s = %s(%s.ReadString())\n", varName, gt, readerName)
			return
		case bt == "bool":
			fmt.Fprintf(sb, "\t%s = %s(%s.ReadBool())\n", varName, gt, readerName)
			return
		}
	}

	switch pType {
	case "[]byte", "TypeBytes", "Array<Uint8>", "Array<byte>":
		fmt.Fprintf(sb, "\t%s = %s.ReadBytes()\n", varName, readerName)
	case "bool", "Bool":
		fmt.Fprintf(sb, "\t%s = %s.ReadBool()\n", varName, readerName)
	case "float64", "Float64", "float32", "Float32":
		fmt.Fprintf(sb, "\t%s = %s.ReadFloat64()\n", varName, readerName)
	case "Any", "any":
		if isHost {
			fmt.Fprintf(sb, "\trawVal = %s.ReadAny()\n\tswitch rv := rawVal.(type) {\n\tcase uint32: if obj, err := registry.GetWithAudit(rv); err == nil { %s = obj } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) }\n\tcase ffigo.ErrorData: if rv.Handle != 0 { if obj, err := registry.GetWithAudit(rv.Handle); err == nil { %s = obj } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) } } else { %s = rv }\n\tdefault: %s = rawVal\n\t}\n", readerName, varName, varName, varName, varName, varName, varName)
		} else {
			fmt.Fprintf(sb, "\t%s = %s.ReadAny()\n", varName, readerName)
		}
	default:
		if itemType, ok := readArrayItemType(pType); ok {
			fmt.Fprintf(sb, "\tl_%s := int(%s.ReadUvarint())\n\t%s = make(%s, l_%s)\n\tfor i_%s := 0; i_%s < l_%s; i_%s++ {\n", varName, readerName, varName, toGoType(pType), varName, varName, varName, varName, varName)
			emitReadAssign(sb, fmt.Sprintf("%s[i_%s]", varName, varName), itemType, nil, structs, readerName, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(pType); ok {
			fmt.Fprintf(sb, "\tl_%s := int(%s.ReadUvarint())\n\t%s = make(%s)\n\tfor i_%s := 0; i_%s < l_%s; i_%s++ {\n\t\tvar k %s\n\t\tvar v %s\n", varName, readerName, varName, toGoType(pType), varName, varName, varName, varName, toGoType(kType), toGoType(vType))
			emitReadAssign(sb, "k", kType, nil, structs, readerName, isHost)
			emitReadAssign(sb, "v", vType, nil, structs, readerName, isHost)
			fmt.Fprintf(sb, "\t\t%s[k] = v\n\t}\n", varName)
			return
		}
		if _, ok := structs[pType]; ok {
			fMap := make(map[string]string)
			getFields(structs, pType, fMap)
			var fNames []string
			for fn := range fMap {
				fNames = append(fNames, fn)
			}
			sort.Strings(fNames)
			for _, fn := range fNames {
				emitReadAssign(sb, varName+"."+fn, fMap[fn], nil, structs, readerName, isHost)
			}
		} else {
			fmt.Fprintf(sb, "\t// Restoring %s from Handle\n", pType)
			if isHost {
				fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if obj, ok := registry.Get(id); ok { %s = obj.(%s) } else { return nil, fmt.Errorf(\"invalid handle ID: %%d\", id) } }\n", readerName, varName, toGoType(pType))
			} else {
				fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if __p.registry != nil { if obj, ok := __p.registry.Get(id); ok { %s = obj.(%s) } } }\n", readerName, varName, toGoType(pType))
			}
		}
	}
}

func getFields(structs map[string]*ast.StructType, strName string, fieldMap map[string]string) {
	str, ok := structs[strName]
	if !ok {
		return
	}
	for _, f := range str.Fields.List {
		if len(f.Names) == 0 {
			tN := typeToString(f.Type)
			if strings.HasPrefix(tN, "Ptr<") {
				tN = tN[4 : len(tN)-1]
			}
			getFields(structs, tN, fieldMap)
		}
	}
	for _, f := range str.Fields.List {
		if len(f.Names) > 0 {
			for _, name := range f.Names {
				if ast.IsExported(name.Name) {
					fieldMap[name.Name] = typeToString(f.Type)
				}
			}
		}
	}
}

func isPrimitive(name string) bool {
	switch name {
	case "Int64", "Float64", "String", "Bool", "Uint8", "Any", "Error", "Void", "TypeBytes":
		return true
	}
	return false
}

func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.ArrayType:
		return fmt.Sprintf("Array<%s>", typeToString(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", typeToString(t.Key), typeToString(t.Value))
	case *ast.StarExpr:
		return fmt.Sprintf("Ptr<%s>", typeToString(t.X))
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "Any"
		}
		return "interface{}"
	case *ast.Ellipsis:
		return fmt.Sprintf("Array<%s>", typeToString(t.Elt))
	default:
		return "Any"
	}
}

func emitReverseRead(sb *strings.Builder, varName, pType, sourceName string) {
	goType := toGoType(pType)
	if pType == "error" {
		fmt.Fprintf(sb, "\t\tif %s != nil { if raw := %s.Interface(); raw != nil { if ed, ok := raw.(ffigo.ErrorData); ok && (ed.Message != \"\" || ed.Handle != 0) { %s = ed } else if s, ok := raw.(string); ok && s != \"\" { %s = fmt.Errorf(\"%%s\", s) } } }\n", sourceName, sourceName, varName, varName)
		return
	}
	if pType == "Any" || pType == "any" {
		fmt.Fprintf(sb, "\t\tif %s != nil { %s = %s.Interface() }\n", sourceName, varName, sourceName)
		return
	}
	fmt.Fprintf(sb, "\t\tif %s != nil { if raw := %s.Interface(); raw != nil {\n", sourceName, sourceName)
	switch pType {
	case "Int64", "int64":
		fmt.Fprintf(sb, "\t\t\tswitch v := raw.(type) { case int64: %s = %s(v); case float64: %s = %s(v) }\n", varName, goType, varName, goType)
	case "String", "string":
		fmt.Fprintf(sb, "\t\t\tif v, ok := raw.(string); ok { %s = v }\n", varName)
	case "Bool", "bool":
		fmt.Fprintf(sb, "\t\t\tif v, ok := raw.(bool); ok { %s = v }\n", varName)
	case "Float64", "float64":
		fmt.Fprintf(sb, "\t\t\tif v, ok := raw.(float64); ok { %s = v }\n", varName)
	default:
		fmt.Fprintf(sb, "\t\t\tif v, ok := raw.(%s); ok { %s = v }\n", goType, varName)
	}
	fmt.Fprintf(sb, "\t\t} }\n")
}

func toGoType(pType string) string {
	if strings.HasPrefix(pType, "Ptr<") {
		return "*" + toGoType(pType[4:len(pType)-1])
	}
	if strings.HasPrefix(pType, "Array<") {
		inner := pType[6 : len(pType)-1]
		if inner == "Uint8" || inner == "byte" || inner == "uint8" {
			return "[]byte"
		}
		return "[]" + toGoType(inner)
	}
	if strings.HasPrefix(pType, "Map<") {
		inner := pType[4 : len(pType)-1]
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			return "map[" + toGoType(strings.TrimSpace(parts[0])) + "]" + toGoType(strings.TrimSpace(parts[1]))
		}
	}
	switch pType {
	case "Uint32", "uint32":
		return "uint32"
	case "Uint16", "uint16":
		return "uint16"
	case "Uint8", "byte", "uint8":
		return "uint8"
	case "Int", "int":
		return "int"
	case "Int64", "int64":
		return "int64"
	case "Int32", "int32":
		return "int32"
	case "Int16", "int16":
		return "int16"
	case "Int8", "int8":
		return "int8"
	case "Uint", "uint":
		return "uint"
	case "String", "string":
		return "string"
	case "Bool", "bool":
		return "bool"
	case "Float64", "float64":
		return "float64"
	case "Float32", "float32":
		return "float32"
	case "context.Context", "Context":
		return "context.Context"
	case "Any", "any", "interface{}":
		return "any"
	case "TypeBytes":
		return "[]byte"
	case "error":
		return "error"
	default:
		return pType
	}
}

func readArrayItemType(pType string) (string, bool) {
	if strings.HasPrefix(pType, "Array<") && strings.HasSuffix(pType, ">") {
		return pType[6 : len(pType)-1], true
	}
	return "", false
}

func readMapKeyValueTypes(pType string) (string, string, bool) {
	if strings.HasPrefix(pType, "Map<") && strings.HasSuffix(pType, ">") {
		inner := pType[4 : len(pType)-1]
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
		}
	}
	return "", "", false
}

func zeroValue(t string) string {
	if strings.HasPrefix(t, "Ptr<") || strings.HasPrefix(t, "Array<") || t == "Any" || t == "any" || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") || t == "error" {
		return "nil"
	}
	switch t {
	case "Uint32", "uint32", "Int", "int", "Int64", "int64", "Int32", "int32":
		return "0"
	case "String", "string":
		return "\"\""
	case "Bool", "bool":
		return "false"
	case "Float64", "float64":
		return "0.0"
	case "TypeBytes":
		return "nil"
	default:
		return t + "{}"
	}
}

func findMethodsForStruct(files []*ast.File, structName string) []*ast.FuncDecl {
	var res []*ast.FuncDecl
	for _, f := range files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) > 0 {
				recvType := fd.Recv.List[0].Type
				if star, ok := recvType.(*ast.StarExpr); ok {
					recvType = star.X
				}
				if ident, ok := recvType.(*ast.Ident); ok && ident.Name == structName {
					if fd.Name.IsExported() {
						res = append(res, fd)
					}
				}
			}
		}
	}
	return res
}

func synthesizeInterface(methods []*ast.FuncDecl, addReceiver bool) *ast.InterfaceType {
	iface := &ast.InterfaceType{
		Methods: &ast.FieldList{},
	}
	for _, md := range methods {
		ft := *md.Type
		newParams := make([]*ast.Field, 0, len(ft.Params.List)+1)

		if addReceiver {
			hasContext := false
			if len(ft.Params.List) > 0 {
				pType := typeToString(ft.Params.List[0].Type)
				if pType == "context.Context" || pType == "Context" {
					hasContext = true
				}
			}

			// Ensure receiver has a name
			recvField := *md.Recv.List[0]
			if len(recvField.Names) == 0 {
				recvField.Names = []*ast.Ident{ast.NewIdent("recv")}
			}

			if hasContext {
				newParams = append(newParams, ft.Params.List[0])
				newParams = append(newParams, &recvField)
				newParams = append(newParams, ft.Params.List[1:]...)
			} else {
				newParams = append(newParams, &recvField)
				newParams = append(newParams, ft.Params.List...)
			}
			ft.Params = &ast.FieldList{List: newParams}
		}

		iface.Methods.List = append(iface.Methods.List, &ast.Field{
			Names: []*ast.Ident{md.Name},
			Type:  &ft,
			Doc:   md.Doc,
		})
	}
	return iface
}
func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.BasicLit:
		return t.Value
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.BinaryExpr:
		return exprToString(t.X) + " " + t.Op.String() + " " + exprToString(t.Y)
	default:
		return ""
	}
}
