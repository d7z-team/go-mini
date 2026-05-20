package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

func generatedFuncSpec(vmType func(ast.Expr) string) func(*ast.FuncType) string {
	return func(funcType *ast.FuncType) string {
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
}

func generatedAsyncReturn(funcType *ast.FuncType) (ast.Expr, string, bool) {
	if funcType == nil || funcType.Results == nil || len(funcType.Results.List) != 1 {
		return nil, "", false
	}
	elem, ok := asyncElemExpr(funcType.Results.List[0].Type)
	if !ok {
		return nil, "", false
	}
	return elem, typeToString(elem), true
}

func generatedMethodHasReceiver(funcType *ast.FuncType, isStruct bool, methodsPrefix string, displayTypeName func(string) string) bool {
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
	if isStruct {
		field := funcType.Params.List[paramIdx]
		return len(field.Names) > 0 && field.Names[0].Name == "__recv"
	}
	receiverType := displayTypeName(typeToString(funcType.Params.List[paramIdx].Type))
	if inner, ok := refElementType(receiverType); ok {
		receiverType = inner
	}
	expectedType := displayTypeName(methodsPrefix)
	return receiverType == expectedType
}

func writeGeneratedBoundRegistrations(sb *strings.Builder, indent string, iface *ast.InterfaceType, name, fixedPrefix, moduleName, methodsPrefix string, isStruct bool, displayTypeName func(string) string) {
	for i, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		methodName := method.Names[0].Name
		funcType := method.Type.(*ast.FuncType)
		routePrefix := fixedPrefix
		if !isStruct && moduleName != "" && methodsPrefix != "" && !generatedMethodHasReceiver(funcType, isStruct, methodsPrefix, displayTypeName) {
			routePrefix = moduleName
		}
		fmt.Fprintf(sb, "%sregistrar.RegisterFFISchema(\"%s%s%s\", bridge, %s_FFI_Schemas[%d].MethodID, %s_FFI_Schemas[%d].Sig, %s_FFI_Schemas[%d].Doc)\n",
			indent, routePrefix, ".", methodName, name, i, name, i, name, i)
	}
}

func buildGeneratedStructSchemaLiteral(iface *ast.InterfaceType, structs map[string]*ast.StructType, structName string, includeFields, includeMethods bool, displayTypeName func(string) string, funcSpec func(*ast.FuncType) string) string {
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
