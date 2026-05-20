package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

func generateCode(spec *ast.TypeSpec, structs map[string]*ast.StructType, interfaces map[string]*ast.InterfaceType, meta targetMeta, constants map[string]string, schemas *schemaRegistry, ownedStructs map[string]bool) string {
	name := spec.Name.Name
	iface, err := flattenInterfaceType(name, spec.Type.(*ast.InterfaceType), interfaces)
	if err != nil {
		panic(fmt.Sprintf("expand interface %s failed: %v", name, err))
	}

	var sb strings.Builder
	methodsPrefix := meta.methodsPrefix
	moduleName := meta.moduleName
	isStruct := meta.structTarget
	isModule := moduleName != ""
	currentOwned := ""
	if isStruct && meta.methodsPrefix != "" {
		currentOwned = meta.methodsPrefix
	}

	displayResolver := newDisplayTypeResolver(moduleName, iface, structs, methodsPrefix, ownedStructs, currentOwned)
	displayTypeName := func(typeName string) string { return displayResolver.NormalizeTypeString(typeName) }
	methodRoutePrefix := func(typeName string) string { return displayTypeName(typeName) }
	vmType := func(expr ast.Expr) string { return displayResolver.VMType(expr) }
	funcSpec := generatedFuncSpec(vmType)
	interfaceSchemaLiteral := buildInterfaceSchemaLiteral(iface, funcSpec)
	interfaceSchemaVar := interfaceSchemaVarName(displayTypeName(name))
	if !isStruct && meta.interfaceMarked {
		fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeInterfaceSpec(\"%s\")\n\n", interfaceSchemaVar, interfaceSchemaLiteral)
		fmt.Fprintf(&sb, "func Register%sSchema(executor interface{ RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec) }) {\n", name)
		fmt.Fprintf(&sb, "\texecutor.RegisterInterfaceSchema(\"%s\", %s)\n", displayTypeName(name), interfaceSchemaVar)
		fmt.Fprintf(&sb, "}\n\n")
	}
	if !isStruct && meta.interfaceMarked {
		return sb.String()
	}
	fixedPrefix := moduleName
	if methodsPrefix != "" {
		fixedPrefix = methodRoutePrefix(methodsPrefix)
	}

	if methodsPrefix != "" {
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			funcType := method.Type.(*ast.FuncType)
			if generatedMethodHasReceiver(funcType, isStruct, methodsPrefix, displayTypeName) {
				continue
			}
			if moduleName != "" {
				continue
			}
			panic(fmt.Sprintf("ffigen:methods validation failed! Interface '%s' method '%s' must use receiver '%s' (or declare ffigen:module for module-level functions).", name, method.Names[0].Name, methodsPrefix))
		}
	}

	referencedSet := collectReferencedStructSet(iface, structs, ownedStructs, currentOwned)
	referencedStructs := referencedSet.ordered
	referencedSchemaVars := make(map[string]string, len(referencedStructs))
	hostOpaqueSchemaName := ""
	if methodsPrefix != "" {
		hostOpaqueSchemaName = methodsPrefix
		if isStruct {
			hostOpaqueSchemaName = name
		}
		referencedSet.ownership[hostOpaqueSchemaName] = "StructOwnershipHostOpaque"
	}
	if schemas != nil {
		for _, structName := range referencedStructs {
			if hostOpaqueSchemaName != "" && displayTypeName(structName) == displayTypeName(hostOpaqueSchemaName) {
				continue
			}
			ownership := referencedSet.ownership[structName]
			if ownership == "" {
				ownership = "StructOwnershipVMValue"
			}
			includeFields := ownership != "StructOwnershipHostOpaque"
			referencedSchemaVars[structName] = schemas.Ensure(displayTypeName(structName), ownership, buildGeneratedStructSchemaLiteral(iface, structs, structName, includeFields, false, displayTypeName, funcSpec))
		}
	}
	selfSchemaVar := ""
	if schemas != nil && methodsPrefix != "" {
		includeFields := false
		schemaName := hostOpaqueSchemaName
		selfSchemaVar = schemas.Ensure(displayTypeName(schemaName), "StructOwnershipHostOpaque", buildGeneratedStructSchemaLiteral(iface, structs, schemaName, includeFields, true, displayTypeName, funcSpec))
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
			hasCopyBack := hasCopyBackParam(funcType)

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
			if _, asyncElemType, ok := generatedAsyncReturn(funcType); ok {
				fmt.Fprintf(&sb, "{\n\treturn ffigo.AsyncFunc[%s](func(ctx context.Context, done ffigo.Completion[%s]) (func(), error) {\n", toGoType(asyncElemType), toGoType(asyncElemType))
				fmt.Fprintf(&sb, "\t\treturn nil, fmt.Errorf(\"ffigen: proxy async call %s.%s is not supported\")\n", name, methodName)
				fmt.Fprintf(&sb, "\t})\n}\n\n")
				continue
			}

			const wireBufName = "wireBuf"
			fmt.Fprintf(&sb, "{\n\t%s := ffigo.GetBuffer()\n\tdefer ffigo.ReleaseBuffer(%s)\n\n", wireBufName, wireBufName)
			argIdx = 0
			type copyBackParam struct {
				name   string
				kind   string
				vmType string
			}
			copyBackVars := make([]copyBackParam, 0)
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
							fmt.Fprintf(&sb, "\t%s.WriteUvarint(uint64(len(%s)))\n", wireBufName, argName)
							fmt.Fprintf(&sb, "\tfor _, item := range %s {\n", argName)
							emitWrite(&sb, "item", itemType, param.Type.(*ast.Ellipsis).Elt, structs, wireBufName, moduleName, false)
							fmt.Fprintf(&sb, "\t}\n")
						} else {
							switch copyBackKind(param.Type) {
							case "bytes":
								copyBackVars = append(copyBackVars, copyBackParam{name: argName, kind: "bytes", vmType: "TypeBytes"})
							case "array":
								copyBackVars = append(copyBackVars, copyBackParam{name: argName, kind: "array", vmType: vmType(param.Type)})
								fmt.Fprintf(&sb, "\tif %s == nil {\n", argName)
								fmt.Fprintf(&sb, "\t\t%s.WriteUvarint(0)\n", wireBufName)
								fmt.Fprintf(&sb, "\t} else {\n")
								emitWrite(&sb, argName+".Value", vmType(param.Type), param.Type, structs, wireBufName, moduleName, false)
								fmt.Fprintf(&sb, "\t}\n")
								argIdx++
								continue
							}
							emitWrite(&sb, argName, pType, param.Type, structs, wireBufName, moduleName, false)
						}
						argIdx++
					} else {
						for _, pName := range param.Names {
							if _, ok := param.Type.(*ast.Ellipsis); ok {
								itemType, _ := readArrayItemType(pType)
								fmt.Fprintf(&sb, "\t%s.WriteUvarint(uint64(len(%s)))\n", wireBufName, pName.Name)
								fmt.Fprintf(&sb, "\tfor _, item := range %s {\n", pName.Name)
								emitWrite(&sb, "item", itemType, param.Type.(*ast.Ellipsis).Elt, structs, wireBufName, moduleName, false)
								fmt.Fprintf(&sb, "\t}\n")
							} else {
								switch copyBackKind(param.Type) {
								case "bytes":
									copyBackVars = append(copyBackVars, copyBackParam{name: pName.Name, kind: "bytes", vmType: "TypeBytes"})
								case "array":
									copyBackVars = append(copyBackVars, copyBackParam{name: pName.Name, kind: "array", vmType: vmType(param.Type)})
									fmt.Fprintf(&sb, "\tif %s == nil {\n", pName.Name)
									fmt.Fprintf(&sb, "\t\t%s.WriteUvarint(0)\n", wireBufName)
									fmt.Fprintf(&sb, "\t} else {\n")
									emitWrite(&sb, pName.Name+".Value", vmType(param.Type), param.Type, structs, wireBufName, moduleName, false)
									fmt.Fprintf(&sb, "\t}\n")
									argIdx++
									continue
								}
								emitWrite(&sb, pName.Name, pType, param.Type, structs, wireBufName, moduleName, false)
							}
							argIdx++
						}
					}
				}
			}

			needsRetBuf := funcType.Results != nil && len(funcType.Results.List) > 0
			fmt.Fprintf(&sb, "\n\t__ret, err := __p.bridge.Call(%s, &ffigo.FFICallRequest{MethodID: MethodID_%s_%s, Args: append([]byte(nil), %s.Bytes()...)})\n", contextVarName, name, methodName, wireBufName)
			if needsRetBuf || hasErr || hasCopyBack {
				fmt.Fprintf(&sb, "\tretData, syncErr := ffigo.SyncBytes(__ret)\n")
				fmt.Fprintf(&sb, "\tif err == nil { err = syncErr }\n")
				fmt.Fprintf(&sb, "\t_ = retData\n")
			} else {
				fmt.Fprintf(&sb, "\tif syncErr := func() error { _, syncErr := ffigo.SyncBytes(__ret); return syncErr }(); err == nil { err = syncErr }\n")
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

			if needsRetBuf || hasCopyBack {
				fmt.Fprintf(&sb, "\tretBuf := ffigo.NewReader(retData)\n")
			}
			if hasCopyBack {
				fmt.Fprintf(&sb, "\tcopyBackCount := int(retBuf.ReadUvarint())\n")
				fmt.Fprintf(&sb, "\tif copyBackCount != %d { panic(fmt.Sprintf(\"ffigen: %s.%s copy-back mismatch: %%d\", copyBackCount)) }\n", len(copyBackVars), name, methodName)
				for _, copyBackVar := range copyBackVars {
					switch copyBackVar.kind {
					case "bytes":
						fmt.Fprintf(&sb, "\tif %s == nil { panic(\"ffigen: nil BytesRef passed to %s.%s\") }\n", copyBackVar.name, name, methodName)
						fmt.Fprintf(&sb, "\t%s.Value = retBuf.ReadBytes()\n", copyBackVar.name)
					case "array":
						fmt.Fprintf(&sb, "\tif %s == nil { panic(\"ffigen: nil ArrayRef passed to %s.%s\") }\n", copyBackVar.name, name, methodName)
						fmt.Fprintf(&sb, "\tcopyBackBuf_%s := ffigo.NewReader(retBuf.ReadBytes())\n", copyBackVar.name)
						tmpVar := "copyBack_" + copyBackVar.name
						fmt.Fprintf(&sb, "\tvar %s %s\n", tmpVar, toGoType(copyBackVar.vmType))
						emitReadAssign(&sb, tmpVar, copyBackVar.vmType, nil, structs, "copyBackBuf_"+copyBackVar.name, moduleName, false)
						fmt.Fprintf(&sb, "\t%s.Value = %s\n", copyBackVar.name, tmpVar)
					}
				}
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
					emitReadAssign(&sb, varName, rType, result.Type, structs, "retBuf", moduleName, false)
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
	fmt.Fprintf(&sb, "func %sHostRouter(ctx context.Context, impl %s, registry *ffigo.HandleRegistry, methodID uint32, methodName string, args []byte) (ffigo.FFIReturn, error) {\n", name, implType)
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
		hasCopyBack := hasCopyBackParam(funcType)
		hasContext := false
		if funcType.Params != nil && len(funcType.Params.List) > 0 {
			pType := typeToString(funcType.Params.List[0].Type)
			if pType == "context.Context" || pType == "Context" {
				hasContext = true
			}
		}

		fmt.Fprintf(&sb, "\tcase MethodID_%s_%s:\n", name, methodName)
		type copyBackParam struct {
			name   string
			kind   string
			vmType string
			expr   ast.Expr
		}
		var paramVars []string
		copyBackVars := make([]copyBackParam, 0)
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
					if copyBackKind(param.Type) == "array" {
						tmpVar := argName + "Value"
						fmt.Fprintf(&sb, "\t\tvar %s %s\n", tmpVar, toGoType(vmType(param.Type)))
						emitReadAssign(&sb, tmpVar, vmType(param.Type), nil, structs, "reqBuf", moduleName, true)
						fmt.Fprintf(&sb, "\t\t%s = &%s{Value: %s}\n", argName, strings.TrimPrefix(goType, "*"), tmpVar)
						copyBackVars = append(copyBackVars, copyBackParam{name: argName, kind: "array", vmType: vmType(param.Type), expr: param.Type})
					} else {
						emitReadAssign(&sb, argName, pType, param.Type, structs, "reqBuf", moduleName, true)
						if copyBackKind(param.Type) == "bytes" {
							copyBackVars = append(copyBackVars, copyBackParam{name: argName, kind: "bytes", vmType: "TypeBytes"})
						}
					}
					if isVariadic {
						paramVars = append(paramVars, argName+"...")
					} else {
						paramVars = append(paramVars, argName)
					}
					argIdx++
				} else {
					for _, pName := range param.Names {
						fmt.Fprintf(&sb, "\t\tvar %s %s\n", pName.Name, goType)
						if copyBackKind(param.Type) == "array" {
							tmpVar := pName.Name + "Value"
							fmt.Fprintf(&sb, "\t\tvar %s %s\n", tmpVar, toGoType(vmType(param.Type)))
							emitReadAssign(&sb, tmpVar, vmType(param.Type), nil, structs, "reqBuf", moduleName, true)
							fmt.Fprintf(&sb, "\t\t%s = &%s{Value: %s}\n", pName.Name, strings.TrimPrefix(goType, "*"), tmpVar)
							copyBackVars = append(copyBackVars, copyBackParam{name: pName.Name, kind: "array", vmType: vmType(param.Type), expr: param.Type})
						} else {
							emitReadAssign(&sb, pName.Name, pType, param.Type, structs, "reqBuf", moduleName, true)
							if copyBackKind(param.Type) == "bytes" {
								copyBackVars = append(copyBackVars, copyBackParam{name: pName.Name, kind: "bytes", vmType: "TypeBytes"})
							}
						}
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
		if isStruct && methodsPrefix != "" && generatedMethodHasReceiver(funcType, isStruct, methodsPrefix, displayTypeName) {
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
		if elemExpr, elemType, ok := generatedAsyncReturn(funcType); ok {
			goElemType := toGoType(elemType)
			fmt.Fprintf(&sb, "\t\treturn ffigo.AsyncValue[%s](r0, func(resBuf *ffigo.Buffer, value %s) error {\n", goElemType, goElemType)
			if tupleItems, tupleOK := tuple2ElemExprs(elemExpr); tupleOK {
				emitWrite(&sb, "value.V0", vmType(tupleItems[0]), tupleItems[0], structs, "resBuf", moduleName, true)
				emitWrite(&sb, "value.V1", vmType(tupleItems[1]), tupleItems[1], structs, "resBuf", moduleName, true)
			} else if vmType(elemExpr) != "Void" {
				emitWrite(&sb, "value", vmType(elemExpr), elemExpr, structs, "resBuf", moduleName, true)
			}
			fmt.Fprintf(&sb, "\t\t\treturn nil\n\t\t}), nil\n")
			continue
		}
		fmt.Fprintf(&sb, "\t\tresBuf := ffigo.GetBuffer()\n")
		if hasCopyBack {
			fmt.Fprintf(&sb, "\t\tresBuf.WriteUvarint(uint64(%d))\n", len(copyBackVars))
			for _, copyBackVar := range copyBackVars {
				switch copyBackVar.kind {
				case "bytes":
					fmt.Fprintf(&sb, "\t\tif %s == nil { resBuf.WriteBytes(nil) } else { resBuf.WriteBytes(%s.Value) }\n", copyBackVar.name, copyBackVar.name)
				case "array":
					fmt.Fprintf(&sb, "\t\tcopyBackBuf_%s := ffigo.GetBuffer()\n", copyBackVar.name)
					fmt.Fprintf(&sb, "\t\tif %s != nil {\n", copyBackVar.name)
					emitWrite(&sb, copyBackVar.name+".Value", copyBackVar.vmType, copyBackVar.expr, structs, "copyBackBuf_"+copyBackVar.name, moduleName, true)
					fmt.Fprintf(&sb, "\t\t}\n")
					fmt.Fprintf(&sb, "\t\tresBuf.WriteBytes(copyBackBuf_%s.Bytes())\n", copyBackVar.name)
					fmt.Fprintf(&sb, "\t\tffigo.ReleaseBuffer(copyBackBuf_%s)\n", copyBackVar.name)
				}
			}
		}
		if funcType.Results != nil {
			for i, result := range funcType.Results.List {
				if typeToString(result.Type) == "error" {
					fmt.Fprintf(&sb, "\t\tif err != nil {\n")
					fmt.Fprintf(&sb, "\t\t\tif registry != nil {\n\t\t\t\tresBuf.WriteRawError(err.Error(), registry.Register(err))\n\t\t\t} else {\n\t\t\t\tresBuf.WriteRawError(err.Error(), 0)\n\t\t\t}\n")
					fmt.Fprintf(&sb, "\t\t} else {\n\t\t\tresBuf.WriteRawError(\"\", 0)\n\t\t}\n")
				} else {
					emitWrite(&sb, fmt.Sprintf("r%d", i), typeToString(result.Type), result.Type, structs, "resBuf", moduleName, true)
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
		modes := funcParamModes(method.Type.(*ast.FuncType))
		if len(modes) > 0 {
			fmt.Fprintf(&sb, "\t{\"%s\", %d, runtime.MustParseRuntimeFuncSigWithModes(\"%s\", %s), \"%s\"},\n", methodName, i+1, funcSpec(method.Type.(*ast.FuncType)), strings.Join(modes, ", "), doc)
		} else {
			fmt.Fprintf(&sb, "\t{\"%s\", %d, runtime.MustParseRuntimeFuncSig(\"%s\"), \"%s\"},\n", methodName, i+1, funcSpec(method.Type.(*ast.FuncType)), doc)
		}
	}
	fmt.Fprintf(&sb, "}\n\n")

	fmt.Fprintf(&sb, "type %s_Bridge struct {\n\tImpl %s\n\tRegistry *ffigo.HandleRegistry\n}\n\n", name, implType)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {\n", name)
	fmt.Fprintf(&sb, "\tif req == nil { return nil, fmt.Errorf(\"ffigen: missing FFI request\") }\n")
	fmt.Fprintf(&sb, "\treturn %sHostRouter(ctx, b.Impl, b.Registry, req.MethodID, \"\", req.Args)\n}\n\n", name)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {\n", name)
	fmt.Fprintf(&sb, "\tif req == nil { return nil, fmt.Errorf(\"ffigen: missing FFI request\") }\n")
	fmt.Fprintf(&sb, "\treturn %sHostRouter(ctx, b.Impl, b.Registry, 0, req.Method, req.Args)\n}\n\n", name)
	fmt.Fprintf(&sb, "func (b *%s_Bridge) DestroyHandle(handle uint32) error {\n\tif b.Registry != nil { b.Registry.Remove(handle) }\n\treturn nil\n}\n\n", name)

	if schemas == nil {
		for _, structName := range referencedStructs {
			if isStruct && structName == name {
				continue
			}
			ownership := referencedSet.ownership[structName]
			if ownership == "" {
				ownership = "StructOwnershipVMValue"
			}
			includeFields := ownership != "StructOwnershipHostOpaque"
			fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(\"%s\", runtime.%s, \"%s\")\n\n",
				structSchemaVarName(displayTypeName(structName)),
				displayTypeName(structName),
				ownership,
				buildGeneratedStructSchemaLiteral(iface, structs, structName, includeFields, false, displayTypeName, funcSpec),
			)
		}
	}

	if isStruct && methodsPrefix != "" {
		// Method Set registration for STRUCT: NO 'impl' parameter
		if schemas == nil {
			fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(\"%s\", runtime.StructOwnershipHostOpaque, \"%s\")\n\n", structSchemaVarName(displayTypeName(name)), displayTypeName(name), buildGeneratedStructSchemaLiteral(iface, structs, name, false, true, displayTypeName, funcSpec))
		}
		fmt.Fprintf(&sb, "func Register%s(executor interface{ RegisterConstant(string, string) }, registry *ffigo.HandleRegistry) {\n", name)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: nil, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tregistrar, ok := executor.(interface{ RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string); RegisterStructSchema(string, *runtime.RuntimeStructSpec); RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec) })\n")
		fmt.Fprintf(&sb, "\tif !ok { panic(\"ffigen: executor does not support schema FFI registration\") }\n")
		if !isStruct && meta.interfaceMarked {
			fmt.Fprintf(&sb, "\tRegister%sSchema(registrar)\n", name)
		}
		fmt.Fprintf(&sb, "\tregisterStructSchema := func(name string, spec *runtime.RuntimeStructSpec) {\n")
		fmt.Fprintf(&sb, "\t\tregistrar.RegisterStructSchema(name, spec)\n")
		fmt.Fprintf(&sb, "\t}\n")
		writeGeneratedBoundRegistrations(&sb, "\t", iface, name, fixedPrefix, moduleName, methodsPrefix, isStruct, displayTypeName)
		for _, structName := range referencedStructs {
			if structName == name {
				continue
			}
			schemaVar := structSchemaVarName(displayTypeName(structName))
			if schemas != nil {
				schemaVar = referencedSchemaVars[structName]
			}
			fmt.Fprintf(&sb, "\tregisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), schemaVar)
		}
		selfVar := structSchemaVarName(displayTypeName(name))
		if schemas != nil {
			selfVar = selfSchemaVar
		}
		fmt.Fprintf(&sb, "\tregisterStructSchema(\"%s\", %s)\n", displayTypeName(name), selfVar)
		fmt.Fprintf(&sb, "}\n")
	} else if isModule || methodsPrefix != "" {
		// Module or Interface-based Methods: REQUIRES 'impl'
		if methodsPrefix != "" && schemas == nil {
			fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(\"%s\", runtime.StructOwnershipHostOpaque, \"%s\")\n\n", structSchemaVarName(displayTypeName(methodsPrefix)), displayTypeName(methodsPrefix), buildGeneratedStructSchemaLiteral(iface, structs, "", false, true, displayTypeName, funcSpec))
		}
		fmt.Fprintf(&sb, "func Register%s(executor interface{ RegisterConstant(string, string) }, impl %s, registry *ffigo.HandleRegistry) {\n", name, implType)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: impl, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tregistrar, ok := executor.(interface{ RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string); RegisterStructSchema(string, *runtime.RuntimeStructSpec); RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec) })\n")
		fmt.Fprintf(&sb, "\tif !ok { panic(\"ffigen: executor does not support schema FFI registration\") }\n")
		if !isStruct && meta.interfaceMarked {
			fmt.Fprintf(&sb, "\tRegister%sSchema(registrar)\n", name)
		}
		if methodsPrefix != "" || len(referencedStructs) > 0 {
			fmt.Fprintf(&sb, "\tregisterStructSchema := func(name string, spec *runtime.RuntimeStructSpec) {\n")
			fmt.Fprintf(&sb, "\t\tregistrar.RegisterStructSchema(name, spec)\n")
			fmt.Fprintf(&sb, "\t}\n")
		}
		writeGeneratedBoundRegistrations(&sb, "\t", iface, name, fixedPrefix, moduleName, methodsPrefix, isStruct, displayTypeName)

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
			for _, structName := range referencedStructs {
				if structName == methodsPrefix {
					continue
				}
				schemaVar := structSchemaVarName(displayTypeName(structName))
				if schemas != nil {
					schemaVar = referencedSchemaVars[structName]
				}
				fmt.Fprintf(&sb, "\tregisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), schemaVar)
			}
			selfVar := structSchemaVarName(displayTypeName(methodsPrefix))
			if schemas != nil {
				selfVar = selfSchemaVar
			}
			fmt.Fprintf(&sb, "\tregisterStructSchema(\"%s\", %s)\n", displayTypeName(methodsPrefix), selfVar)
		} else if len(referencedStructs) > 0 {
			for _, structName := range referencedStructs {
				if isStruct && structName == name {
					continue
				}
				schemaVar := structSchemaVarName(displayTypeName(structName))
				if schemas != nil {
					schemaVar = referencedSchemaVars[structName]
				}
				fmt.Fprintf(&sb, "\tregisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), schemaVar)
			}
		}
		fmt.Fprintf(&sb, "}\n")
	} else {
		// Generic Library registration: Requires 'impl' and explicit prefix
		fmt.Fprintf(&sb, "func Register%sLibrary(executor interface{ RegisterConstant(string, string) }, prefix string, impl %s, registry *ffigo.HandleRegistry) {\n", name, implType)
		fmt.Fprintf(&sb, "\tbridge := &%s_Bridge{Impl: impl, Registry: registry}\n", name)
		fmt.Fprintf(&sb, "\tregistrar, ok := executor.(interface{ RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string); RegisterStructSchema(string, *runtime.RuntimeStructSpec); RegisterInterfaceSchema(string, *runtime.RuntimeInterfaceSpec) })\n")
		fmt.Fprintf(&sb, "\tif !ok { panic(\"ffigen: executor does not support schema FFI registration\") }\n")
		if !isStruct && meta.interfaceMarked {
			fmt.Fprintf(&sb, "\tRegister%sSchema(registrar)\n", name)
		}
		if len(referencedStructs) > 0 {
			fmt.Fprintf(&sb, "\tregisterStructSchema := func(name string, spec *runtime.RuntimeStructSpec) {\n")
			fmt.Fprintf(&sb, "\t\tregistrar.RegisterStructSchema(name, spec)\n")
			fmt.Fprintf(&sb, "\t}\n")
		}
		fmt.Fprintf(&sb, "\tfor _, m := range %s_FFI_Schemas {\n\t\tregistrar.RegisterFFISchema(prefix+\".\"+m.Name, bridge, m.MethodID, m.Sig, m.Doc)\n\t}\n", name)
		for _, structName := range referencedStructs {
			schemaVar := structSchemaVarName(displayTypeName(structName))
			if schemas != nil {
				schemaVar = referencedSchemaVars[structName]
			}
			fmt.Fprintf(&sb, "\tregisterStructSchema(\"%s\", %s)\n", displayTypeName(structName), schemaVar)
		}
		fmt.Fprintf(&sb, "}\n")
	}

	return sb.String()
}
