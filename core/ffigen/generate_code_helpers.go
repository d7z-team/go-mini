package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func generatedFuncSpec(vmType func(ast.Expr) string) func(*ast.FuncType) string {
	return func(funcType *ast.FuncType) string {
		var params []miniast.FunctionParam
		var variadic bool
		if funcType.Params != nil {
			for i, p := range funcType.Params.List {
				pType := miniast.GoMiniType(vmType(p.Type))
				if i == 0 && (pType == "context.Context" || pType == "Context") {
					continue
				}
				if _, ok := p.Type.(*ast.Ellipsis); ok {
					variadic = true
					if elem, ok := pType.ReadArrayItemType(); ok {
						pType = elem
					}
				}
				if len(p.Names) == 0 {
					params = append(params, miniast.FunctionParam{Type: pType})
				} else {
					for range p.Names {
						params = append(params, miniast.FunctionParam{Type: pType})
					}
				}
			}
		}
		var results []miniast.GoMiniType
		if funcType.Results != nil {
			for _, r := range funcType.Results.List {
				t := miniast.GoMiniType(vmType(r.Type))
				if t == "error" {
					t = miniast.TypeError
				}
				count := len(r.Names)
				if count == 0 {
					count = 1
				}
				for range count {
					results = append(results, t)
				}
			}
		}
		actualRet := miniast.TypeVoid
		if len(results) > 0 {
			actualRet = miniast.CreateTupleType(results...)
		}
		return string(miniast.CreateFunctionType(params, actualRet, variadic))
	}
}

func (g *Generator) generatedAsyncReturn(funcType *ast.FuncType) (ast.Expr, string, bool) {
	if funcType == nil || funcType.Results == nil || len(funcType.Results.List) != 1 {
		return nil, "", false
	}
	elem, ok := g.asyncElemExpr(funcType.Results.List[0].Type)
	if !ok {
		return nil, "", false
	}
	return elem, g.typeToString(elem), true
}

func (g *Generator) generatedMethodHasReceiver(funcType *ast.FuncType, isStruct bool, methodsPrefix string, displayTypeName func(string) string) bool {
	hasContext := false
	if funcType.Params != nil && len(funcType.Params.List) > 0 {
		pType := g.typeToString(funcType.Params.List[0].Type)
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
	if isStruct {
		field := funcType.Params.List[paramIdx]
		return len(field.Names) > 0 && field.Names[0].Name == "__recv"
	}
	receiverType := displayTypeName(g.typeToString(funcType.Params.List[paramIdx].Type))
	if inner, ok := ffigo.RefElementType(receiverType); ok {
		receiverType = inner
	}
	expectedType := displayTypeName(methodsPrefix)
	return receiverType == expectedType
}

type generatedParam struct {
	Name         string
	RawType      string
	VMType       string
	ProxyGoType  string
	HostGoType   string
	Expr         ast.Expr
	CopyBackKind string
	Variadic     bool
	Context      bool
}

type generatedResult struct {
	Index   int
	RawType string
	GoType  string
	Expr    ast.Expr
	Error   bool
}

type generatedMethod struct {
	Name        string
	SchemaIndex int
	MethodID    int
	FuncType    *ast.FuncType
	Params      []generatedParam
	Results     []generatedResult
	Modes       []string
	Doc         string

	HasContext  bool
	ContextVar  string
	HasCopyBack bool
	HasError    bool
	HasReceiver bool
	HasInput    bool
	NeedsRawVal bool

	AsyncExpr   ast.Expr
	AsyncGoType string
}

func (g *Generator) buildGeneratedMethods(iface *ast.InterfaceType, isStruct bool, methodsPrefix string, displayTypeName func(string) string, vmType func(ast.Expr) string) []generatedMethod {
	if iface == nil || iface.Methods == nil {
		return nil
	}
	methods := make([]generatedMethod, 0, len(iface.Methods.List))
	for i, field := range iface.Methods.List {
		if len(field.Names) == 0 {
			continue
		}
		methodName := field.Names[0].Name
		funcType := field.Type.(*ast.FuncType)
		method := generatedMethod{
			Name:        methodName,
			SchemaIndex: len(methods),
			MethodID:    i + 1,
			FuncType:    funcType,
			ContextVar:  "context.Background()",
			HasReceiver: g.generatedMethodHasReceiver(funcType, isStruct, methodsPrefix, displayTypeName),
		}
		if field.Doc != nil {
			method.Doc = strings.TrimSpace(strings.ReplaceAll(field.Doc.Text(), "\n", " "))
		}
		if elemExpr, elemType, ok := g.generatedAsyncReturn(funcType); ok {
			method.AsyncExpr = elemExpr
			method.AsyncGoType = g.toGoType(elemType)
		}

		argIdx := 0
		if funcType.Params != nil {
			for paramIdx, param := range funcType.Params.List {
				rawType := g.typeToString(param.Type)
				paramIsContext := paramIdx == 0 && isContextType(rawType)
				if !paramIsContext && g.unsupportedInterfaceExpr(param.Type) {
					panic(fmt.Sprintf("ffigen: interface parameter %s.%s is not supported; use any or *T/HostRef<T>", methodName, rawType))
				}
				variadic := false
				proxyGoType := g.toGoType(rawType)
				hostGoType := proxyGoType
				if _, ok := param.Type.(*ast.Ellipsis); ok {
					variadic = true
					proxyGoType = "..." + strings.TrimPrefix(proxyGoType, "[]")
					hostGoType = "[]" + strings.TrimPrefix(hostGoType, "[]")
				}
				names := param.Names
				if len(names) == 0 {
					names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("arg%d", argIdx))}
				}
				for _, name := range names {
					paramPlan := generatedParam{
						Name:         name.Name,
						RawType:      rawType,
						VMType:       vmType(param.Type),
						ProxyGoType:  proxyGoType,
						HostGoType:   hostGoType,
						Expr:         param.Type,
						CopyBackKind: g.copyBackKind(param.Type),
						Variadic:     variadic,
						Context:      paramIsContext,
					}
					if paramIsContext {
						method.HasContext = true
						method.ContextVar = paramPlan.Name
					} else {
						method.HasInput = true
						mode := "runtime.FFIParamIn"
						switch paramPlan.CopyBackKind {
						case "bytes":
							mode = "runtime.FFIParamInOutBytes"
							method.HasCopyBack = true
						case "array":
							mode = "runtime.FFIParamInOutArray"
							method.HasCopyBack = true
						}
						method.Modes = append(method.Modes, mode)
						if paramNeedsRawVal(paramPlan) {
							method.NeedsRawVal = true
						}
					}
					method.Params = append(method.Params, paramPlan)
					argIdx++
				}
			}
		}

		resultIdx := 0
		if funcType.Results != nil {
			for _, result := range funcType.Results.List {
				rawType := g.typeToString(result.Type)
				if rawType != "error" && g.unsupportedInterfaceExpr(result.Type) {
					panic(fmt.Sprintf("ffigen: interface result %s.%s is not supported; use any or *T/HostRef<T>", methodName, rawType))
				}
				count := len(result.Names)
				if count == 0 {
					count = 1
				}
				for range count {
					resultPlan := generatedResult{
						Index:   resultIdx,
						RawType: rawType,
						GoType:  g.toGoType(rawType),
						Expr:    result.Type,
						Error:   rawType == "error",
					}
					if resultPlan.Error {
						method.HasError = true
					}
					method.Results = append(method.Results, resultPlan)
					resultIdx++
				}
			}
		}
		methods = append(methods, method)
	}
	return methods
}

func isContextType(typeName string) bool {
	return typeName == "context.Context" || typeName == "Context"
}

func paramNeedsRawVal(param generatedParam) bool {
	if param.Context {
		return false
	}
	if param.RawType == "Any" || param.RawType == "any" || strings.Contains(param.RawType, "<Any>") || strings.Contains(param.RawType, "<any>") {
		return true
	}
	if param.Variadic {
		itemType, _ := readArrayItemType(param.RawType)
		return itemType == "Any" || itemType == "any"
	}
	return false
}

func (m generatedMethod) copyBackParams() []copyBackParam {
	params := make([]copyBackParam, 0)
	for _, param := range m.Params {
		switch param.CopyBackKind {
		case "bytes":
			params = append(params, copyBackParam{name: param.Name, kind: "bytes", vmType: "TypeBytes", expr: param.Expr})
		case "array":
			params = append(params, copyBackParam{name: param.Name, kind: "array", vmType: param.VMType, expr: param.Expr})
		}
	}
	return params
}

func (m generatedMethod) resultNames() []string {
	names := make([]string, 0, len(m.Results))
	for _, result := range m.Results {
		if result.Error {
			names = append(names, "err")
		} else {
			names = append(names, fmt.Sprintf("r%d", result.Index))
		}
	}
	return names
}

type copyBackParam struct {
	name   string
	kind   string
	vmType string
	expr   ast.Expr
}

func (g *Generator) writeGeneratedSurfaceRoutes(sb *strings.Builder, indent, schemaVar, boundVar, bridgeVar string, methods []generatedMethod, name, fixedPrefix, moduleName, methodsPrefix string, isStruct bool) {
	for _, method := range methods {
		routePrefix := fixedPrefix
		packageMember := !isStruct && methodsPrefix == ""
		if !isStruct && moduleName != "" && methodsPrefix != "" && !method.HasReceiver {
			routePrefix = moduleName
			packageMember = true
		} else if methodsPrefix != "" || isStruct {
			packageMember = false
		}
		routeName := routePrefix + "." + method.Name
		item := fmt.Sprintf("%s_FFI_Schemas[%d]", name, method.SchemaIndex)
		if schemaVar != "" && packageMember {
			fmt.Fprintf(sb, "%s%s.AddFunc(%q, %q, %q, %s.MethodID, %s.Sig, %s.Doc)\n",
				indent, schemaVar, routePrefix, method.Name, routeName, item, item, item)
		}
		if boundVar == "" {
			continue
		}
		route := fmt.Sprintf("runtime.FFIRoute{Name: %q, Bridge: %s, MethodID: %s.MethodID, FuncSig: %s.Sig, Doc: %s.Doc}",
			routeName, bridgeVar, item, item, item)
		if packageMember {
			fmt.Fprintf(sb, "%s%s.AddRoute(%q, %q, %s)\n", indent, boundVar, routePrefix, method.Name, route)
			continue
		}
		fmt.Fprintf(sb, "%s%s.Routes[%q] = %s\n", indent, boundVar, routeName, route)
	}
}

func (g *Generator) buildGeneratedStructSchemaLiteral(iface *ast.InterfaceType, structs map[string]*ast.StructType, structName string, includeFields, includeMethods bool, displayTypeName func(string) string, funcSpec func(*ast.FuncType) string) string {
	var members []miniast.StructMemberType
	if includeFields {
		if str, ok := structs[structName]; ok {
			var keys []string
			fMap := make(map[string]string)
			g.getFields(structs, structName, fMap)
			for k := range fMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				members = append(members, miniast.StructMemberType{Name: k, Type: miniast.GoMiniType(displayTypeName(fMap[k]))})
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
			members = append(members, miniast.StructMemberType{Name: mName, Type: miniast.GoMiniType(funcSpec(method.Type.(*ast.FuncType)))})
		}
	}
	return string(miniast.CreateStructType(members))
}

func (g *Generator) generateGlobalsCode(globals []globalValue) string {
	if len(globals) == 0 {
		return ""
	}
	items := append([]globalValue(nil), globals...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].meta.PackagePath != items[j].meta.PackagePath {
			return items[i].meta.PackagePath < items[j].meta.PackagePath
		}
		return items[i].meta.Name < items[j].meta.Name
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "func SurfaceGlobals() *surface.Bundle {\n")
	fmt.Fprintf(&sb, "\tschema := runtime.NewFFISurfaceSchema()\n")
	for i, item := range items {
		fmt.Fprintf(&sb, "\tspec%d := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType(%q), ReadOnly: true}\n", i, item.meta.MiniType)
		fmt.Fprintf(&sb, "\tschema.AddValue(%q, %q, spec%d)\n", item.meta.PackagePath, item.meta.Name, i)
	}
	fmt.Fprintf(&sb, "\treturn surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {\n")
	fmt.Fprintf(&sb, "\t\tbound := runtime.NewBoundFFISurface(schema)\n")
	for i, item := range items {
		elem, ok := hostRefElementType(item.meta.MiniType)
		if !ok {
			panic("ffigen:global currently requires HostRef<T> canonical type")
		}
		fmt.Fprintf(&sb, "\t\tvalue%d, err := (runtime.StaticHostRefProvider{ElementType: runtime.TypeSpec(%q), Value: %s, Bridge: &%s_Bridge{Registry: ctx.Registry}}).Bind(ctx)\n", i, elem, item.variable, localTypeName(elem))
		fmt.Fprintf(&sb, "\t\tif err != nil { return nil, err }\n")
		fmt.Fprintf(&sb, "\t\tbound.AddPackageValue(%q, %q, spec%d, value%d)\n", item.meta.PackagePath, item.meta.Name, i, i)
	}
	fmt.Fprintf(&sb, "\t\treturn bound, nil\n")
	fmt.Fprintf(&sb, "\t})\n")
	fmt.Fprintf(&sb, "}\n\n")
	return sb.String()
}

func hostRefElementType(miniType string) (string, bool) {
	miniType = strings.TrimSpace(miniType)
	spec := runtime.TypeSpec(miniType)
	if err := spec.ValidateCanonical(); err != nil {
		panic(fmt.Sprintf("ffigen:global type %q is not canonical: %v", miniType, err))
	}
	elem, ok := spec.HostRefElement()
	if !ok {
		return "", false
	}
	return elem.String(), true
}

func localTypeName(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if idx := strings.LastIndex(typeName, "."); idx >= 0 && idx < len(typeName)-1 {
		return typeName[idx+1:]
	}
	if idx := strings.LastIndex(typeName, "/"); idx >= 0 && idx < len(typeName)-1 {
		return typeName[idx+1:]
	}
	return typeName
}
