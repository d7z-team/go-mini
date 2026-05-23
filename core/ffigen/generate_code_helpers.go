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
					results = append(results, miniast.TypeError)
				} else {
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

func (g *Generator) writeGeneratedSurfaceRoutes(sb *strings.Builder, indent, schemaVar, boundVar, bridgeVar string, iface *ast.InterfaceType, name, fixedPrefix, moduleName, methodsPrefix string, isStruct bool, displayTypeName func(string) string) {
	for i, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		methodName := method.Names[0].Name
		funcType := method.Type.(*ast.FuncType)
		routePrefix := fixedPrefix
		packageMember := !isStruct && methodsPrefix == ""
		if !isStruct && moduleName != "" && methodsPrefix != "" && !g.generatedMethodHasReceiver(funcType, isStruct, methodsPrefix, displayTypeName) {
			routePrefix = moduleName
			packageMember = true
		} else if methodsPrefix != "" || isStruct {
			packageMember = false
		}
		routeName := routePrefix + "." + methodName
		item := fmt.Sprintf("%s_FFI_Schemas[%d]", name, i)
		if schemaVar != "" && packageMember {
			fmt.Fprintf(sb, "%s%s.AddFunc(%q, %q, %q, %s.MethodID, %s.Sig, %s.Doc)\n",
				indent, schemaVar, routePrefix, methodName, routeName, item, item, item)
		}
		if boundVar == "" {
			continue
		}
		route := fmt.Sprintf("runtime.FFIRoute{Name: %q, Bridge: %s, MethodID: %s.MethodID, FuncSig: %s.Sig, Doc: %s.Doc}",
			routeName, bridgeVar, item, item, item)
		if packageMember {
			fmt.Fprintf(sb, "%s%s.AddRoute(%q, %q, %s)\n", indent, boundVar, routePrefix, methodName, route)
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
