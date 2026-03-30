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
	pkgName  = flag.String("pkg", "", "package name")
	basePath = flag.String("path", "", "full import path of the current package (optional, will try to derive if empty)")
	outFile  = flag.String("out", "", "output file")
	module   = flag.String("module", "", "logical module name (e.g. 'time' or 'os')")
	scanAll  = flag.Bool("scan", false, "scan all .go files in the directory for methods")

	// 类型推导上下文
	typeInfo     *types.Info
	fset         *token.FileSet
	knownImports map[string]string
)

func main() {
	flag.Parse()
	if *pkgName == "" || *outFile == "" {
		fmt.Println("Usage: ffigen -pkg <name> [-path <full_import_path>] -out <file> [input files...]")
		os.Exit(1)
	}

	if len(flag.Args()) == 0 {
		fmt.Println("Error: no input files provided")
		os.Exit(1)
	}

	// Try to derive basePath if not provided
	if *basePath == "" {
		// Run go list in the directory of the first input file
		dir := filepath.Dir(flag.Args()[0])
		cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deriving import path in %s: %v\n", dir, err)
			fmt.Fprintf(os.Stderr, "Please provide -path explicitly.\n")
			os.Exit(1)
		}
		derived := strings.TrimSpace(string(out))
		if derived == "" || derived == "." {
			fmt.Fprintf(os.Stderr, "Error: derived import path is empty or invalid. Please provide -path explicitly.\n")
			os.Exit(1)
		}
		basePath = &derived
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
		if *scanAll && !seenDirs[dir] {
			seenDirs[dir] = true
			// Parse all .go files in this directory
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

	var ifaceSpecs []*ast.TypeSpec
	globalConsts := make(map[string]map[string]string) // pkg -> name -> value
	structs := make(map[string]*ast.StructType)
	knownImports = make(map[string]string)

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
						hasFfigenModule := false
						if gd.Doc != nil {
							for _, line := range gd.Doc.List {
								if strings.Contains(line.Text, "ffigen:module") {
									hasFfigenModule = true
									break
								}
							}
						}

						if hasFfigenModule || *scanAll {
							for i, name := range valSpec.Names {
								if name.IsExported() {
									if i < len(valSpec.Values) {
										val := exprToString(valSpec.Values[i])
										if val != "" {
											if globalConsts[*pkgName] == nil {
												globalConsts[*pkgName] = make(map[string]string)
											}
											globalConsts[*pkgName][name.Name] = val
										}
									}
								}
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
						if typeSpec.Doc == nil {
							typeSpec.Doc = gd.Doc
						}

						hasFfigen := false
						if typeSpec.Doc != nil {
							for _, line := range typeSpec.Doc.List {
								if strings.Contains(line.Text, "ffigen:") {
									hasFfigen = true
									break
								}
							}
						}

						if _, ok := typeSpec.Type.(*ast.InterfaceType); ok {
							ifaceSpecs = append(ifaceSpecs, typeSpec)
						} else if _, ok := typeSpec.Type.(*ast.StructType); ok {
							if hasFfigen {
								isModule := false
								if typeSpec.Doc != nil {
									for _, line := range typeSpec.Doc.List {
										if strings.Contains(line.Text, "ffigen:module") {
											isModule = true
											break
										}
									}
								}

								methods := findMethodsForStruct(allFiles, typeSpec.Name.Name)
								if len(methods) > 0 {
									// Only add receiver if it's NOT a module (i.e. it's ffigen:methods)
									virtualIface := synthesizeInterface(methods, !isModule)
									virtualSpec := *typeSpec
									virtualSpec.Type = virtualIface
									if virtualSpec.Doc == nil {
										virtualSpec.Doc = &ast.CommentGroup{}
									}
									virtualSpec.Doc.List = append(virtualSpec.Doc.List, &ast.Comment{Text: "// ffigen:struct"})
									ifaceSpecs = append(ifaceSpecs, &virtualSpec)
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
	anyReverse := false
	for _, spec := range ifaceSpecs {
		isReverse := false
		isStruct := false
		if spec.Doc != nil {
			for _, line := range spec.Doc.List {
				if strings.Contains(line.Text, "ffigen:reverse") {
					isReverse = true
					anyReverse = true
				}
				if strings.Contains(line.Text, "ffigen:struct") {
					isStruct = true
				}
			}
		}
		interfaces = append(interfaces, generateCode(*pkgName, spec, structs, isReverse, isStruct, globalConsts[*pkgName]))
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
	for alias, path := range stdPackages {
		if alias == "runtime" && !anyReverse {
			continue
		}
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

func generateCode(pkg string, spec *ast.TypeSpec, structs map[string]*ast.StructType, isReverse, isStruct bool, constants map[string]string) string {
	name := spec.Name.Name
	iface := spec.Type.(*ast.InterfaceType)

	var sb strings.Builder
	fixedPrefix := ""
	methodsPrefix := ""
	isModule := false
	if spec.Doc != nil {
		for _, line := range spec.Doc.List {
			if strings.Contains(line.Text, "ffigen:module") {
				isModule = true
				parts := strings.Fields(line.Text)
				for i, p := range parts {
					if p == "ffigen:module" && i+1 < len(parts) {
						fixedPrefix = parts[i+1]
						break
					}
				}
			}
			if strings.Contains(line.Text, "ffigen:methods") {
				parts := strings.Fields(line.Text)
				for i, p := range parts {
					if p == "ffigen:methods" {
						if i+1 < len(parts) {
							methodsPrefix = parts[i+1]
						} else {
							methodsPrefix = name
						}
						fixedPrefix = "__method_" + resolveCanonicalType(methodsPrefix)
						break
					}
				}
			}
		}
	}

	if methodsPrefix != "" && !isReverse {
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

			paramIdx := 0
			if hasContext {
				paramIdx = 1
			}

			if funcType.Params == nil || len(funcType.Params.List) <= paramIdx {
				panic(fmt.Sprintf("ffigen:methods validation failed! Interface '%s' has ffigen:methods '%s', but method '%s' has no receiver parameter. The first parameter (or second, if the first is context.Context) must be the receiver.", name, methodsPrefix, method.Names[0].Name))
			}

			receiverType := typeToString(funcType.Params.List[paramIdx].Type)
			receiverTypeClean := strings.TrimPrefix(receiverType, "Ptr<")
			receiverTypeClean = strings.TrimSuffix(receiverTypeClean, ">")

			// Validation should still use the Go-source visible name (e.g. other.Page)
			if receiverTypeClean != methodsPrefix {
				panic(fmt.Sprintf("ffigen:methods validation failed! Interface '%s' method '%s' expects receiver type '%s', but ffigen:methods specifies '%s'. Please ensure the ffigen:methods prefix exactly matches the receiver type (including package prefix).", name, method.Names[0].Name, receiverTypeClean, methodsPrefix))
			}
		}
	}

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

	fmt.Fprintf(&sb, "var %s_FFI_Metadata = []struct {\n\tName     string\n\tMethodID uint32\n\tSpec     string\n\tDoc      string\n}{\n", name)
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
		fmt.Fprintf(&sb, "\t{\"%s\", %d, \"%s\", \"%s\"},\n", methodName, i+1, getSpec(method.Type.(*ast.FuncType)), doc)
	}
	fmt.Fprintf(&sb, "}\n\n")

	fmt.Fprintf(&sb, "type %s_Bridge struct {\n\tImpl %s\n\tRegistry *ffigo.HandleRegistry\n}\n\n", name, implType)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {\n", name)
	fmt.Fprintf(&sb, "\treturn %sHostRouter(ctx, b.Impl, b.Registry, methodID, \"\", args)\n}\n\n", name)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {\n", name)
	fmt.Fprintf(&sb, "\treturn %sHostRouter(ctx, b.Impl, b.Registry, 0, method, args)\n}\n\n", name)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) DestroyHandle(handle uint32) error {\n\tif b.Registry != nil { b.Registry.Remove(handle) }\n\treturn nil\n}\n\n", name)

	if isStruct && methodsPrefix != "" {
		// Method Set registration for STRUCT: NO 'impl' parameter
		fmt.Fprintf(&sb, "func Register%s(executor interface{ RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string); RegisterStructSpec(string, ast.GoMiniType); RegisterConstant(string, string) }, registry *ffigo.HandleRegistry) {\n", name)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: nil, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tprefix := \"%s\"\n\tsep := \".\"\n\tif strings.HasPrefix(prefix, \"__method_\") { sep = \"_\" }\n", fixedPrefix)
		fmt.Fprintf(&sb, "\tfor _, m := range %s_FFI_Metadata {\n\t\texecutor.RegisterFFI(prefix+sep+m.Name, bridge, m.MethodID, ast.GoMiniType(m.Spec), m.Doc)\n\t}\n", name)

		// Register struct metadata
		fmt.Fprintf(&sb, "\t// Register struct metadata for validation and code completion\n")
		var fieldsSB strings.Builder
		fieldsSB.WriteString("struct { ")
		if str, ok := structs[name]; ok {
			var keys []string
			fMap := make(map[string]string)
			getFields(structs, name, fMap)
			for k := range fMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&fieldsSB, "%s %s; ", k, fMap[k])
			}
			_ = str
		}
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			mName := method.Names[0].Name
			fmt.Fprintf(&fieldsSB, "%s %s; ", mName, getSpec(method.Type.(*ast.FuncType)))
		}
		fieldsSB.WriteString("}")
		fmt.Fprintf(&sb, "\texecutor.RegisterStructSpec(\"%s\", \"%s\")\n", resolveCanonicalType(name), fieldsSB.String())
		fmt.Fprintf(&sb, "}\n")
	} else if isModule || methodsPrefix != "" {
		// Module or Interface-based Methods: REQUIRES 'impl'
		fmt.Fprintf(&sb, "func Register%s(executor interface{ RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string); RegisterStructSpec(string, ast.GoMiniType); RegisterConstant(string, string) }, impl %s, registry *ffigo.HandleRegistry) {\n", name, implType)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: impl, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tprefix := \"%s\"\n\tsep := \".\"\n\tif strings.HasPrefix(prefix, \"__method_\") { sep = \"_\" }\n", fixedPrefix)
		fmt.Fprintf(&sb, "\tfor _, m := range %s_FFI_Metadata {\n\t\texecutor.RegisterFFI(prefix+sep+m.Name, bridge, m.MethodID, ast.GoMiniType(m.Spec), m.Doc)\n\t}\n", name)

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
			// Register as a struct if it's a method set
			var fieldsSB strings.Builder
			fieldsSB.WriteString("struct { ")
			for _, method := range iface.Methods.List {
				if len(method.Names) == 0 {
					continue
				}
				mName := method.Names[0].Name
				fmt.Fprintf(&fieldsSB, "%s %s; ", mName, getSpec(method.Type.(*ast.FuncType)))
			}
			fieldsSB.WriteString("}")
			fmt.Fprintf(&sb, "\texecutor.RegisterStructSpec(\"%s\", \"%s\")\n", resolveCanonicalType(methodsPrefix), fieldsSB.String())
		}
		fmt.Fprintf(&sb, "}\n")
	} else {
		// Generic Library registration: Requires 'impl' and explicit prefix
		fmt.Fprintf(&sb, "func Register%s%sLibrary(executor interface{ RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string); RegisterStructSpec(string, ast.GoMiniType); RegisterConstant(string, string) }, prefix string, impl %s, registry *ffigo.HandleRegistry) {\n", strings.ToUpper(pkg), name, implType)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: impl, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tsep := \".\"\n\tif strings.HasPrefix(prefix, \"__method_\") { sep = \"_\" }\n")
		fmt.Fprintf(&sb, "\tfor _, m := range %s_FFI_Metadata {\n\t\texecutor.RegisterFFI(prefix+sep+m.Name, bridge, m.MethodID, ast.GoMiniType(m.Spec), m.Doc)\n\t}\n}\n", name)
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
			fmt.Fprintf(&sb, "\tresVar, err := __p.program.InvokeCallable(__p.ctx, __p.callable, \"%s\", args)\n\t_ = err\n\t_ = resVar\n", mName)
			if fType.Results != nil {
				var rStmts []string
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
				fmt.Fprintf(&sb, "\treturn\n")
			}
			fmt.Fprintf(&sb, "}\n\n")
		}
	}

	return sb.String()
}

func emitWrite(sb *strings.Builder, prefix, pType string, expr ast.Expr, structs map[string]*ast.StructType, bufName string, isHost bool) {
	if strings.HasPrefix(pType, "Ptr<") {
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

func getSpec(funcType *ast.FuncType) string {
	var params []string
	if funcType.Params != nil {
		for i, p := range funcType.Params.List {
			pType := toVMType(p.Type)
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
			t := toVMType(r.Type)
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

func resolveCanonicalType(name string) string {
	if !strings.Contains(name, ".") {
		if *module != "" {
			return *module + "." + name
		}
		if *basePath != "" {
			return toLogicalPath(*basePath) + "." + name
		}
		return name
	}
	parts := strings.SplitN(name, ".", 2)
	prefix := parts[0]
	if fullPath, ok := knownImports[prefix]; ok {
		return toLogicalPath(fullPath) + "." + parts[1]
	}
	return name
}

func toLogicalPath(fullPath string) string {
	const stdPrefix = "gopkg.d7z.net/go-mini/core/ffilib/"
	if strings.HasPrefix(fullPath, stdPrefix) {
		rel := strings.TrimPrefix(fullPath, stdPrefix)
		parts := strings.Split(rel, "/")
		for i, p := range parts {
			if strings.HasSuffix(p, "lib") {
				parts[i] = strings.TrimSuffix(p, "lib")
			}
		}
		return strings.Join(parts, "/")
	}
	return fullPath
}
func toVMType(expr ast.Expr) string {
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
	case *ast.Ident:
		name := t.Name
		switch name {
		case "int", "int8", "int16", "int32", "int64", "uint", "uint16", "uint32":
			return "Int64"
		case "float64", "float32":
			return "Float64"
		case "string":
			return "String"
		case "bool":
			return "Bool"
		case "byte", "uint8":
			return "Uint8"
		case "any", "interface{}":
			return "Any"
		case "error":
			return "Error"
		}
		if strings.Contains(name, ".") {
			return resolveCanonicalType(name)
		}
		return name
	case *ast.ArrayType:
		if ident, ok := t.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
			return "TypeBytes"
		}
		return fmt.Sprintf("Array<%s>", toVMType(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", toVMType(t.Key), toVMType(t.Value))
	case *ast.StarExpr:
		inner := toVMType(t.X)
		if !strings.Contains(inner, ".") && !isPrimitive(inner) {
			inner = resolveCanonicalType(inner)
		}
		return fmt.Sprintf("Ptr<%s>", inner)
	case *ast.Ellipsis:
		return fmt.Sprintf("Array<%s>", toVMType(t.Elt))
	case *ast.SelectorExpr:
		var name string
		if x, ok := t.X.(*ast.Ident); ok {
			name = x.Name + "." + t.Sel.Name
		} else {
			name = t.Sel.Name
		}
		return resolveCanonicalType(name)
	case *ast.InterfaceType:
		// Interfaces are always handles, so canonicalize methods if possible?
		// Actually ffigen interface processing is elsewhere.
		return "Any"
	default:
		return "Any"
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
