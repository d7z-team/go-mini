package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
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

func (g *Generator) writeGeneratedBoundRegistrations(sb *strings.Builder, indent string, iface *ast.InterfaceType, name, fixedPrefix, moduleName, methodsPrefix string, isStruct bool, displayTypeName func(string) string) {
	for i, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		methodName := method.Names[0].Name
		funcType := method.Type.(*ast.FuncType)
		routePrefix := fixedPrefix
		if !isStruct && moduleName != "" && methodsPrefix != "" && !g.generatedMethodHasReceiver(funcType, isStruct, methodsPrefix, displayTypeName) {
			routePrefix = moduleName
		}
		fmt.Fprintf(sb, "%sregistrar.RegisterFFISchema(\"%s%s%s\", bridge, %s_FFI_Schemas[%d].MethodID, %s_FFI_Schemas[%d].Sig, %s_FFI_Schemas[%d].Doc)\n",
			indent, routePrefix, ".", methodName, name, i, name, i, name, i)
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
