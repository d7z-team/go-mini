package ffigen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

func (g *Generator) parseDirectoryFiles(dir, absOutFile string) ([]*ast.File, error) {
	if g.fset == nil {
		g.fset = token.NewFileSet()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		absFilePath, _ := filepath.Abs(filePath)
		if absOutFile != "" && absFilePath == absOutFile {
			continue
		}
		file, err := parser.ParseFile(g.fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parsing file %s: %w", filePath, err)
		}
		if isGeneratedFile(entry.Name(), file) {
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

func (g *Generator) parseFileInputs(args []string, outFile string) ([]*ast.File, []*ast.File, error) {
	g.fset = token.NewFileSet()
	seenDirs := make(map[string]bool)
	parsedFiles := make(map[string]*ast.File)
	absOutFile, _ := filepath.Abs(outFile)
	var allFiles []*ast.File

	for _, arg := range args {
		absPath, err := filepath.Abs(arg)
		if err != nil {
			return nil, nil, fmt.Errorf("getting absolute path for %s: %w", arg, err)
		}
		dir := filepath.Dir(absPath)
		if !seenDirs[dir] {
			seenDirs[dir] = true
			dirFiles, err := g.parseDirectoryFiles(dir, absOutFile)
			if err != nil {
				return nil, nil, err
			}
			for _, file := range dirFiles {
				pos := g.fset.Position(file.Pos())
				absFile, _ := filepath.Abs(pos.Filename)
				parsedFiles[absFile] = file
				allFiles = append(allFiles, file)
			}
		}
		if _, ok := parsedFiles[absPath]; !ok {
			file, err := parser.ParseFile(g.fset, absPath, nil, parser.ParseComments)
			if err != nil {
				return nil, nil, fmt.Errorf("parsing explicitly provided file %s: %w", absPath, err)
			}
			if isGeneratedFile(filepath.Base(absPath), file) {
				continue
			}
			parsedFiles[absPath] = file
			allFiles = append(allFiles, file)
		}
	}

	var inputFiles []*ast.File
	for _, arg := range args {
		absPath, _ := filepath.Abs(arg)
		if file, ok := parsedFiles[absPath]; ok {
			inputFiles = append(inputFiles, file)
		}
	}
	return allFiles, inputFiles, nil
}

type packageData struct {
	defaultModule string
	targets       []ffigenTarget
	structs       map[string]*ast.StructType
	interfaces    map[string]*ast.InterfaceType
	constants     map[string]string
	ownedStructs  map[string]bool
}

func (g *Generator) collectPackageData(allFiles, targetFiles []*ast.File) (map[string]bool, packageData) {
	preKnownImports := collectImportAliases(allFiles)
	conf := types.Config{Importer: g.newSourceImporter(), IgnoreFuncBodies: true}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	g.typeInfo = info
	checkPath := g.packagePath
	if checkPath == "" {
		checkPath = g.opts.PackageName
	}
	if _, err := conf.Check(checkPath, g.fset, allFiles, info); err != nil {
		// 类型检查失败不一定意味着无法生成，但我们需要感知这些错误。
		// 在 FFI 生成场景中，如果缺少某些外部依赖，conf.Check 会报错，
		// 只要我们关注的 target 结构是完整的，通常可以继续。
		if !isIgnorableTypeCheckError(err, preKnownImports) {
			fmt.Fprintf(os.Stderr, "ffigen: type check warning in %s: %v\n", checkPath, err)
		}
	}

	g.knownImports = make(map[string]string)
	g.moduleCache = make(map[string]string)
	structs := make(map[string]*ast.StructType)
	interfaces := make(map[string]*ast.InterfaceType)
	globalConsts := make(map[string]string)

	for _, node := range allFiles {
		for _, imp := range node.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			parts := strings.Split(path, "/")
			alias := parts[len(parts)-1]
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			g.knownImports[alias] = path
		}
		ast.Inspect(node, func(n ast.Node) bool {
			gd, ok := n.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					if str, ok := typeSpec.Type.(*ast.StructType); ok {
						structs[typeSpec.Name.Name] = str
					}
					if iface, ok := typeSpec.Type.(*ast.InterfaceType); ok {
						interfaces[typeSpec.Name.Name] = iface
					}
				}
				if valSpec, ok := spec.(*ast.ValueSpec); ok {
					for i, name := range valSpec.Names {
						if !name.IsExported() || i >= len(valSpec.Values) {
							continue
						}
						if val := exprToString(valSpec.Values[i]); val != "" {
							globalConsts[name.Name] = val
						}
					}
				}
			}
			return true
		})
	}

	defaultModule := g.derivePackageDefaultModule(allFiles)
	packageMode := len(allFiles) == len(targetFiles)
	moduleNames := make(map[string]bool)
	var targets []ffigenTarget
	ownedStructs := make(map[string]bool)
	for _, node := range targetFiles {
		ast.Inspect(node, func(n ast.Node) bool {
			gd, ok := n.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				meta := parseTargetMeta(g.resolveTargetDoc(node, gd, typeSpec))
				mat := materializeTargetMeta(meta, defaultModule)
				if mat.moduleName != "" {
					moduleNames[mat.moduleName] = true
				}
				if _, ok := typeSpec.Type.(*ast.InterfaceType); ok {
					if mat.moduleName != "" || !packageMode || mat.interfaceMarked {
						targets = append(targets, ffigenTarget{spec: typeSpec, meta: mat})
					}
					continue
				}
				if _, ok := typeSpec.Type.(*ast.StructType); !ok {
					continue
				}
				if mat.moduleName == "" && !mat.methodsMarked {
					continue
				}
				methodsPrefix := mat.methodsPrefix
				if mat.methodsMarked && methodsPrefix == "" {
					methodsPrefix = typeSpec.Name.Name
				}
				methods := findMethodsForStruct(allFiles, typeSpec.Name.Name)
				if len(methods) == 0 {
					continue
				}
				virtualIface := g.synthesizeInterface(methods, mat.methodsMarked)
				virtualSpec := *typeSpec
				virtualSpec.Type = virtualIface
				targets = append(targets, ffigenTarget{
					spec: &virtualSpec,
					meta: materializeTargetMeta(targetMeta{
						moduleName:    mat.moduleName,
						methodsPrefix: methodsPrefix,
						methodsMarked: mat.methodsMarked,
						structTarget:  true,
					}, defaultModule),
				})
				ownedStructs[typeSpec.Name.Name] = true
			}
			return true
		})
	}

	return moduleNames, packageData{
		defaultModule: defaultModule,
		targets:       targets,
		structs:       structs,
		interfaces:    interfaces,
		constants:     globalConsts,
		ownedStructs:  ownedStructs,
	}
}
