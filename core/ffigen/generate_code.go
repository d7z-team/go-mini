package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

func privateIdent(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	allUpper := true
	for _, r := range name {
		if 'a' <= r && r <= 'z' {
			allUpper = false
			break
		}
	}
	if allUpper {
		return strings.ToLower(name)
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func methodIDConstName(typeName, methodName string) string {
	return "methodID" + typeName + methodName
}

func routeDeclVarName(typeName string) string {
	return privateIdent(typeName) + "Routes"
}

func hostRouterName(typeName string) string {
	return privateIdent(typeName) + "HostRouter"
}

func (g *Generator) generateCode(spec *ast.TypeSpec, structs map[string]*ast.StructType, interfaces map[string]*ast.InterfaceType, interfaceFFI map[string]bool, meta targetMeta, constants map[string]string, schemas *schemaRegistry, ownedStructs map[string]bool) string {
	name := spec.Name.Name
	iface, err := g.flattenInterfaceType(name, spec.Type.(*ast.InterfaceType), interfaces)
	if err != nil {
		panic(fmt.Sprintf("expand interface %s failed: %v", name, err))
	}

	var sb strings.Builder
	methodsPrefix := meta.methodsPrefix
	moduleName := meta.moduleName
	isStruct := meta.structTarget
	isModule := moduleName != ""
	currentOwned := ""
	if isStruct && methodsPrefix != "" {
		currentOwned = methodsPrefix
	}

	displayResolver := g.newDisplayTypeResolver(moduleName)
	displayTypeName := func(typeName string) string { return displayResolver.NormalizeTypeString(typeName) }
	interfaceSchemaVars := buildInterfaceSchemaVars(interfaceFFI, displayTypeName)
	vmType := func(expr ast.Expr) string { return displayResolver.VMType(expr) }
	funcSpec := generatedFuncSpec(vmType)

	if !isStruct && meta.interfaceMarked {
		interfaceSchemaVar := interfaceSchemaVarName(displayTypeName(name))
		fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeInterfaceSpec(%q)\n\n", interfaceSchemaVar, buildInterfaceSchemaLiteral(iface, funcSpec))
		fmt.Fprintf(&sb, "func Surface%sSchema() *surface.Bundle {\n", name)
		fmt.Fprintf(&sb, "\tschema := runtime.NewFFISurfaceSchema()\n")
		fmt.Fprintf(&sb, "\tschema.AddInterface(\"%s\", %s)\n", displayTypeName(name), interfaceSchemaVar)
		fmt.Fprintf(&sb, "\treturn surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {\n")
		fmt.Fprintf(&sb, "\t\tbound := runtime.NewBoundFFISurfaceFromSchema(schema)\n")
		fmt.Fprintf(&sb, "\t\treturn bound, nil\n")
		fmt.Fprintf(&sb, "\t})\n")
		fmt.Fprintf(&sb, "}\n\n")
		return sb.String()
	}

	fixedPrefix := moduleName
	if methodsPrefix != "" {
		fixedPrefix = displayTypeName(methodsPrefix)
	}
	methods := g.buildGeneratedMethods(iface, isStruct, methodsPrefix, displayTypeName, vmType, interfaceSchemaVars, moduleName)
	if methodsPrefix != "" {
		for _, method := range methods {
			if method.HasReceiver || moduleName != "" {
				continue
			}
			panic(fmt.Sprintf("ffigen:methods validation failed! Interface '%s' method '%s' must use receiver '%s' (or declare ffigen:module for module-level functions).", name, method.Name, methodsPrefix))
		}
	}

	referencedSet := g.collectReferencedStructSet(iface, structs, ownedStructs, currentOwned)
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
			referencedSchemaVars[structName] = schemas.Ensure(displayTypeName(structName), ownership, g.buildGeneratedStructSchemaLiteral(iface, structs, structName, includeFields, false, displayTypeName, funcSpec))
		}
	}
	selfSchemaVar := ""
	if schemas != nil && methodsPrefix != "" {
		selfSchemaVar = schemas.Ensure(displayTypeName(hostOpaqueSchemaName), "StructOwnershipHostOpaque", g.buildGeneratedStructSchemaLiteral(iface, structs, hostOpaqueSchemaName, false, true, displayTypeName, funcSpec))
	}

	implType := name
	if isStruct {
		implType = "*" + name
	}
	structSchemaVar := func(structName string) string {
		if schemas != nil {
			if varName := referencedSchemaVars[structName]; varName != "" {
				return varName
			}
		}
		return structSchemaVarName(displayTypeName(structName))
	}
	selfStructSchemaVar := func(structName string) string {
		if schemas != nil && selfSchemaVar != "" {
			return selfSchemaVar
		}
		return structSchemaVarName(displayTypeName(structName))
	}
	emitStructAdds := func(skip ...string) {
		skipSet := make(map[string]bool, len(skip))
		for _, structName := range skip {
			skipSet[structName] = true
		}
		for _, structName := range referencedStructs {
			if skipSet[structName] {
				continue
			}
			fmt.Fprintf(&sb, "\tschema.AddStruct(\"%s\", %s)\n", displayTypeName(structName), structSchemaVar(structName))
		}
	}
	emitConstAdds := func() {
		if !isModule || fixedPrefix == "" || len(constants) == 0 {
			return
		}
		keys := make([]string, 0, len(constants))
		for key := range constants {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&sb, "\tschema.AddConst(\"%s\", %q, ffigo.ToConstantString(%s))\n", fixedPrefix, key, constants[key])
		}
	}
	referencedInterfaces := g.collectReferencedInterfaceNames(methods, moduleName, interfaceSchemaVars)
	emitInterfaceAdds := func() {
		for _, interfaceName := range referencedInterfaces {
			fmt.Fprintf(&sb, "\tschema.AddInterface(\"%s\", %s)\n", interfaceName, interfaceSchemaVars[interfaceName])
		}
	}

	fmt.Fprintf(&sb, "const (\n")
	for _, method := range methods {
		fmt.Fprintf(&sb, "\t%s = %d\n", methodIDConstName(name, method.Name), method.MethodID)
	}
	fmt.Fprintf(&sb, ")\n\n")

	if !isStruct && meta.proxyMarked {
		g.writeProxy(&sb, name, methods, structs, moduleName, interfaceSchemaVars)
	}
	g.writeHostRouter(&sb, name, implType, methods, structs, moduleName, isStruct, methodsPrefix, vmType, interfaceSchemaVars)

	fmt.Fprintf(&sb, "var %s = []runtime.FFIRouteDecl{\n", routeDeclVarName(name))
	for _, method := range methods {
		decl := g.generatedRouteDeclLiteral(method, name, fixedPrefix, moduleName, methodsPrefix, isStruct)
		if len(method.Modes) > 0 {
			fmt.Fprintf(&sb, "\t%s Sig: runtime.MustParseRuntimeFuncSigWithModes(%q, %s), Doc: %q},\n", decl, funcSpec(method.FuncType), strings.Join(method.Modes, ", "), method.Doc)
		} else {
			fmt.Fprintf(&sb, "\t%s Sig: runtime.MustParseRuntimeFuncSig(%q), Doc: %q},\n", decl, funcSpec(method.FuncType), method.Doc)
		}
	}
	fmt.Fprintf(&sb, "}\n\n")

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
			fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(%q, runtime.%s, %q)\n\n",
				structSchemaVar(structName),
				displayTypeName(structName),
				ownership,
				g.buildGeneratedStructSchemaLiteral(iface, structs, structName, includeFields, false, displayTypeName, funcSpec),
			)
		}
	}

	if isStruct && methodsPrefix != "" {
		if schemas == nil {
			fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(%q, runtime.StructOwnershipHostOpaque, %q)\n\n", structSchemaVar(name), displayTypeName(name), g.buildGeneratedStructSchemaLiteral(iface, structs, name, false, true, displayTypeName, funcSpec))
		}
		selfVar := selfStructSchemaVar(name)
		fmt.Fprintf(&sb, "func Surface%s() *surface.Bundle {\n", name)
		fmt.Fprintf(&sb, "\tschema := runtime.NewFFISurfaceSchema()\n")
		fmt.Fprintf(&sb, "\tschema.AddRouteDecls(%s)\n", routeDeclVarName(name))
		emitStructAdds(name)
		emitInterfaceAdds()
		fmt.Fprintf(&sb, "\tschema.AddStruct(\"%s\", %s)\n", displayTypeName(name), selfVar)
		fmt.Fprintf(&sb, "\treturn surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {\n")
		g.writeSchemaRouteBinder(&sb, "\t\t", name, "nil")
		fmt.Fprintf(&sb, "\t\treturn bound, nil\n")
		fmt.Fprintf(&sb, "\t})\n")
		fmt.Fprintf(&sb, "}\n")
		return sb.String()
	}

	if isModule || methodsPrefix != "" {
		if methodsPrefix != "" && schemas == nil {
			fmt.Fprintf(&sb, "var %s = runtime.MustParseRuntimeStructSpec(%q, runtime.StructOwnershipHostOpaque, %q)\n\n", structSchemaVar(methodsPrefix), displayTypeName(methodsPrefix), g.buildGeneratedStructSchemaLiteral(iface, structs, "", false, true, displayTypeName, funcSpec))
		}
		fmt.Fprintf(&sb, "func Surface%s(impl %s) *surface.Bundle {\n", name, implType)
		fmt.Fprintf(&sb, "\tschema := runtime.NewFFISurfaceSchema()\n")
		fmt.Fprintf(&sb, "\tschema.AddRouteDecls(%s)\n", routeDeclVarName(name))
		emitConstAdds()
		if methodsPrefix != "" {
			emitStructAdds(methodsPrefix)
			fmt.Fprintf(&sb, "\tschema.AddStruct(\"%s\", %s)\n", displayTypeName(methodsPrefix), selfStructSchemaVar(methodsPrefix))
		} else {
			emitStructAdds()
		}
		emitInterfaceAdds()
		fmt.Fprintf(&sb, "\treturn surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {\n")
		g.writeSchemaRouteBinder(&sb, "\t\t", name, "impl")
		fmt.Fprintf(&sb, "\t\treturn bound, nil\n")
		fmt.Fprintf(&sb, "\t})\n")
		fmt.Fprintf(&sb, "}\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "func Surface%sLibrary(prefix string, impl %s) *surface.Bundle {\n", name, implType)
	fmt.Fprintf(&sb, "\tschema := runtime.NewFFISurfaceSchema()\n")
	fmt.Fprintf(&sb, "\troutes := make([]runtime.FFIRouteDecl, 0, len(%s))\n", routeDeclVarName(name))
	fmt.Fprintf(&sb, "\tfor _, route := range %s {\n", routeDeclVarName(name))
	fmt.Fprintf(&sb, "\t\troute.PackagePath = prefix\n")
	fmt.Fprintf(&sb, "\t\troute.RouteName = prefix + \".\" + route.MemberName\n")
	fmt.Fprintf(&sb, "\t\troutes = append(routes, route)\n")
	fmt.Fprintf(&sb, "\t}\n")
	fmt.Fprintf(&sb, "\tschema.AddRouteDecls(routes)\n")
	emitStructAdds()
	emitInterfaceAdds()
	fmt.Fprintf(&sb, "\treturn surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {\n")
	g.writeSchemaRouteBinder(&sb, "\t\t", name, "impl")
	fmt.Fprintf(&sb, "\t\treturn bound, nil\n")
	fmt.Fprintf(&sb, "\t})\n")
	fmt.Fprintf(&sb, "}\n")
	return sb.String()
}

func (g *Generator) writeProxy(sb *strings.Builder, name string, methods []generatedMethod, structs map[string]*ast.StructType, moduleName string, interfaceSchemaVars map[string]string) {
	fmt.Fprintf(sb, "type %sProxy struct {\n\tbridge ffigo.FFIBridge\n\tregistry *ffigo.HandleRegistry\n}\n\n", name)
	fmt.Fprintf(sb, "func New%sProxy(bridge ffigo.FFIBridge, registry *ffigo.HandleRegistry) %s {\n\treturn &%sProxy{bridge: bridge, registry: registry}\n}\n\n", name, name, name)

	for _, method := range methods {
		fmt.Fprintf(sb, "func (__p *%sProxy) %s(", name, method.Name)
		paramDecls := make([]string, 0, len(method.Params))
		for _, param := range method.Params {
			paramDecls = append(paramDecls, param.Name+" "+param.ProxyGoType)
		}
		fmt.Fprintf(sb, "%s) ", strings.Join(paramDecls, ", "))
		if len(method.Results) > 0 {
			types := make([]string, 0, len(method.Results))
			for _, result := range method.Results {
				if result.Error {
					types = append(types, "error")
				} else {
					types = append(types, result.GoType)
				}
			}
			fmt.Fprintf(sb, "(%s) ", strings.Join(types, ", "))
		}
		if method.AsyncExpr != nil {
			fmt.Fprintf(sb, "{\n\treturn ffigo.AsyncFunc[%s](func(ctx context.Context, done ffigo.Completion[%s]) (ffigo.WaitHandle, error) {\n", method.AsyncGoType, method.AsyncGoType)
			fmt.Fprintf(sb, "\t\treturn nil, fmt.Errorf(\"ffigen: proxy async call %s.%s is not supported\")\n", name, method.Name)
			fmt.Fprintf(sb, "\t})\n}\n\n")
			continue
		}

		const wireBufName = "wireBuf"
		copyBackVars := method.copyBackParams()
		needsRetBuf := len(method.Results) > 0
		fmt.Fprintf(sb, "{\n\t%s := ffigo.GetBuffer()\n\tdefer ffigo.ReleaseBuffer(%s)\n\n", wireBufName, wireBufName)
		for _, param := range method.Params {
			if param.Context {
				continue
			}
			if param.Variadic {
				itemType, _ := readArrayItemType(param.RawType)
				fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(len(%s)))\n", wireBufName, param.Name)
				fmt.Fprintf(sb, "\tfor _, item := range %s {\n", param.Name)
				g.emitWrite(sb, "item", itemType, param.Expr.(*ast.Ellipsis).Elt, structs, wireBufName, moduleName, interfaceSchemaVars, false)
				fmt.Fprintf(sb, "\t}\n")
				continue
			}
			if param.CopyBackKind == "array" {
				fmt.Fprintf(sb, "\tif %s == nil {\n", param.Name)
				fmt.Fprintf(sb, "\t\t%s.WriteUvarint(0)\n", wireBufName)
				fmt.Fprintf(sb, "\t} else {\n")
				g.emitWrite(sb, param.Name+".Value", param.VMType, param.Expr, structs, wireBufName, moduleName, interfaceSchemaVars, false)
				fmt.Fprintf(sb, "\t}\n")
				continue
			}
			g.emitWrite(sb, param.Name, param.RawType, param.Expr, structs, wireBufName, moduleName, interfaceSchemaVars, false)
		}

		fmt.Fprintf(sb, "\n\t__ret, err := __p.bridge.Call(%s, &ffigo.FFICallRequest{MethodID: %s, Args: append([]byte(nil), %s.Bytes()...)})\n", method.ContextVar, methodIDConstName(name, method.Name), wireBufName)
		if needsRetBuf || method.HasError || method.HasCopyBack {
			fmt.Fprintf(sb, "\tretData, syncErr := ffigo.SyncBytes(__ret)\n")
			fmt.Fprintf(sb, "\tif err == nil { err = syncErr }\n")
			fmt.Fprintf(sb, "\t_ = retData\n")
		} else {
			fmt.Fprintf(sb, "\tif syncErr := func() error { _, syncErr := ffigo.SyncBytes(__ret); return syncErr }(); err == nil { err = syncErr }\n")
		}
		fmt.Fprintf(sb, "\t_ = err\n")

		if method.HasError {
			retValues := make([]string, 0, len(method.Results))
			for _, result := range method.Results {
				if result.Error {
					retValues = append(retValues, "err")
				} else {
					retValues = append(retValues, zeroValue(result.GoType))
				}
			}
			fmt.Fprintf(sb, "\tif err != nil { return %s }\n", strings.Join(retValues, ", "))
		}
		if needsRetBuf || method.HasCopyBack {
			fmt.Fprintf(sb, "\tretBuf := ffigo.NewReader(retData)\n")
		}
		if method.HasCopyBack {
			fmt.Fprintf(sb, "\tcopyBackCount := int(retBuf.ReadUvarint())\n")
			fmt.Fprintf(sb, "\tif copyBackCount != %d { panic(fmt.Sprintf(\"ffigen: %s.%s copy-back mismatch: %%d\", copyBackCount)) }\n", len(copyBackVars), name, method.Name)
			for _, copyBackVar := range copyBackVars {
				switch copyBackVar.kind {
				case "bytes":
					fmt.Fprintf(sb, "\tif %s == nil { panic(\"ffigen: nil BytesRef passed to %s.%s\") }\n", copyBackVar.name, name, method.Name)
					fmt.Fprintf(sb, "\t%s.Value = retBuf.ReadBytes()\n", copyBackVar.name)
				case "array":
					fmt.Fprintf(sb, "\tif %s == nil { panic(\"ffigen: nil ArrayRef passed to %s.%s\") }\n", copyBackVar.name, name, method.Name)
					fmt.Fprintf(sb, "\tcopyBackBuf_%s := ffigo.NewReader(retBuf.ReadBytes())\n", copyBackVar.name)
					tmpVar := "copyBack_" + copyBackVar.name
					fmt.Fprintf(sb, "\tvar %s %s\n", tmpVar, g.toGoType(copyBackVar.vmType))
					g.emitReadAssign(sb, tmpVar, copyBackVar.vmType, nil, structs, "copyBackBuf_"+copyBackVar.name, moduleName, interfaceSchemaVars, false)
					fmt.Fprintf(sb, "\t%s.Value = %s\n", copyBackVar.name, tmpVar)
				}
			}
		}

		retStmt := make([]string, 0, len(method.Results))
		for _, result := range method.Results {
			if result.Error {
				varName := fmt.Sprintf("err_%d", result.Index)
				fmt.Fprintf(sb, "\tvar %s error\n", varName)
				fmt.Fprintf(sb, "\tif retBuf.Available() > 0 {\n")
				fmt.Fprintf(sb, "\t\ted := retBuf.ReadRawError()\n")
				fmt.Fprintf(sb, "\t\tif ed.Message != \"\" || ed.Handle != 0 {\n")
				fmt.Fprintf(sb, "\t\t\tif ed.Handle != 0 && __p.registry != nil {\n")
				fmt.Fprintf(sb, "\t\t\t\tif obj, ok := __p.registry.Get(ed.Handle); ok { %s = obj.(error) } else { %s = ed }\n", varName, varName)
				fmt.Fprintf(sb, "\t\t\t} else { %s = ed }\n", varName)
				fmt.Fprintf(sb, "\t\t}\n\t}\n")
				retStmt = append(retStmt, varName)
				continue
			}
			varName := fmt.Sprintf("v_%d", result.Index)
			fmt.Fprintf(sb, "\tvar %s %s\n", varName, result.GoType)
			g.emitReadAssign(sb, varName, result.RawType, result.Expr, structs, "retBuf", moduleName, interfaceSchemaVars, false)
			retStmt = append(retStmt, varName)
		}
		if len(retStmt) > 0 {
			fmt.Fprintf(sb, "\treturn %s\n", strings.Join(retStmt, ", "))
		} else {
			fmt.Fprintf(sb, "\treturn\n")
		}
		fmt.Fprintf(sb, "}\n\n")
	}
}

func (g *Generator) writeHostRouter(sb *strings.Builder, name, implType string, methods []generatedMethod, structs map[string]*ast.StructType, moduleName string, isStruct bool, methodsPrefix string, vmType func(ast.Expr) string, interfaceSchemaVars map[string]string) {
	fmt.Fprintf(sb, "func %s(ctx context.Context, impl %s, registry *ffigo.HandleRegistry, methodID uint32, methodName string, args []byte) (ffigo.FFIReturn, error) {\n", hostRouterName(name), implType)
	fmt.Fprintf(sb, "\tif methodID == 0 && methodName != \"\" {\n")
	fmt.Fprintf(sb, "\t\tswitch methodName {\n")
	for _, method := range methods {
		fmt.Fprintf(sb, "\t\tcase \"%s\":\n", method.Name)
		fmt.Fprintf(sb, "\t\t\tmethodID = %s\n", methodIDConstName(name, method.Name))
	}
	fmt.Fprintf(sb, "\t\t}\n")
	fmt.Fprintf(sb, "\t}\n\n")

	needsReqBuf := false
	needsRawVal := false
	for _, method := range methods {
		needsReqBuf = needsReqBuf || method.HasInput
		needsRawVal = needsRawVal || method.NeedsRawVal
	}
	if needsReqBuf {
		fmt.Fprintf(sb, "\treqBuf := ffigo.NewReader(args)\n")
	}
	if needsRawVal {
		fmt.Fprintf(sb, "\tvar rawVal any\n\t_ = rawVal\n")
	}
	fmt.Fprintf(sb, "\tswitch methodID {\n")
	for _, method := range methods {
		fmt.Fprintf(sb, "\tcase %s:\n", methodIDConstName(name, method.Name))
		paramVars := make([]string, 0, len(method.Params))
		for _, param := range method.Params {
			if param.Context {
				paramVars = append(paramVars, "ctx")
				continue
			}
			fmt.Fprintf(sb, "\t\tvar %s %s\n", param.Name, param.HostGoType)
			if param.CopyBackKind == "array" {
				tmpVar := param.Name + "Value"
				fmt.Fprintf(sb, "\t\tvar %s %s\n", tmpVar, g.toGoType(param.VMType))
				g.emitReadAssign(sb, tmpVar, param.VMType, nil, structs, "reqBuf", moduleName, interfaceSchemaVars, true)
				fmt.Fprintf(sb, "\t\t%s = &%s{Value: %s}\n", param.Name, strings.TrimPrefix(param.HostGoType, "*"), tmpVar)
			} else {
				g.emitReadAssign(sb, param.Name, param.RawType, param.Expr, structs, "reqBuf", moduleName, interfaceSchemaVars, true)
			}
			if param.Variadic {
				paramVars = append(paramVars, param.Name+"...")
			} else {
				paramVars = append(paramVars, param.Name)
			}
		}

		callPrefix := "impl."
		callParams := paramVars
		if isStruct && methodsPrefix != "" && method.HasReceiver {
			paramIdx := 0
			if method.HasContext {
				paramIdx = 1
			}
			if len(paramVars) > paramIdx {
				callPrefix = paramVars[paramIdx] + "."
				callParams = append([]string{}, paramVars[:paramIdx]...)
				callParams = append(callParams, paramVars[paramIdx+1:]...)
			}
		}
		retVars := method.resultNames()
		if len(retVars) > 0 {
			fmt.Fprintf(sb, "\t\t%s := %s%s(%s)\n", strings.Join(retVars, ", "), callPrefix, method.Name, strings.Join(callParams, ", "))
		} else {
			fmt.Fprintf(sb, "\t\t%s%s(%s)\n", callPrefix, method.Name, strings.Join(callParams, ", "))
		}
		if method.AsyncExpr != nil {
			fmt.Fprintf(sb, "\t\treturn ffigo.AsyncValue[%s](r0, func(resBuf *ffigo.Buffer, value %s) error {\n", method.AsyncGoType, method.AsyncGoType)
			if tupleItems, tupleOK := g.tuple2ElemExprs(method.AsyncExpr); tupleOK {
				g.emitWrite(sb, "value.V0", vmType(tupleItems[0]), tupleItems[0], structs, "resBuf", moduleName, interfaceSchemaVars, true)
				g.emitWrite(sb, "value.V1", vmType(tupleItems[1]), tupleItems[1], structs, "resBuf", moduleName, interfaceSchemaVars, true)
			} else if vmType(method.AsyncExpr) != "Void" {
				g.emitWrite(sb, "value", vmType(method.AsyncExpr), method.AsyncExpr, structs, "resBuf", moduleName, interfaceSchemaVars, true)
			}
			fmt.Fprintf(sb, "\t\t\treturn nil\n\t\t}), nil\n")
			continue
		}

		copyBackVars := method.copyBackParams()
		fmt.Fprintf(sb, "\t\tresBuf := ffigo.GetBuffer()\n")
		if method.HasCopyBack {
			fmt.Fprintf(sb, "\t\tresBuf.WriteUvarint(uint64(%d))\n", len(copyBackVars))
			for _, copyBackVar := range copyBackVars {
				switch copyBackVar.kind {
				case "bytes":
					fmt.Fprintf(sb, "\t\tif %s == nil { resBuf.WriteBytes(nil) } else { resBuf.WriteBytes(%s.Value) }\n", copyBackVar.name, copyBackVar.name)
				case "array":
					fmt.Fprintf(sb, "\t\tcopyBackBuf_%s := ffigo.GetBuffer()\n", copyBackVar.name)
					fmt.Fprintf(sb, "\t\tif %s != nil {\n", copyBackVar.name)
					g.emitWrite(sb, copyBackVar.name+".Value", copyBackVar.vmType, copyBackVar.expr, structs, "copyBackBuf_"+copyBackVar.name, moduleName, interfaceSchemaVars, true)
					fmt.Fprintf(sb, "\t\t}\n")
					fmt.Fprintf(sb, "\t\tresBuf.WriteBytes(copyBackBuf_%s.Bytes())\n", copyBackVar.name)
					fmt.Fprintf(sb, "\t\tffigo.ReleaseBuffer(copyBackBuf_%s)\n", copyBackVar.name)
				}
			}
		}
		for _, result := range method.Results {
			if result.Error {
				fmt.Fprintf(sb, "\t\tif err != nil {\n")
				fmt.Fprintf(sb, "\t\t\tif registry != nil {\n\t\t\t\tresBuf.WriteRawError(err.Error(), registry.Register(err))\n\t\t\t} else {\n\t\t\t\tresBuf.WriteRawError(err.Error(), 0)\n\t\t\t}\n")
				fmt.Fprintf(sb, "\t\t} else {\n\t\t\tresBuf.WriteRawError(\"\", 0)\n\t\t}\n")
				continue
			}
			g.emitWrite(sb, fmt.Sprintf("r%d", result.Index), result.RawType, result.Expr, structs, "resBuf", moduleName, interfaceSchemaVars, true)
		}
		fmt.Fprintf(sb, "\t\treturn resBuf.Bytes(), nil\n")
	}
	fmt.Fprintf(sb, "\tdefault:\n\t\treturn nil, fmt.Errorf(\"unknown method ID %%d\", methodID)\n\t}\n}\n")
}
