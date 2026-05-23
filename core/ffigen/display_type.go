package ffigen

import (
	"go/ast"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (g *Generator) newDisplayTypeResolver(moduleName string) *displayTypeResolver {
	return &displayTypeResolver{gen: g, moduleName: moduleName}
}

func (r *displayTypeResolver) NormalizeTypeString(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return "Any"
	}
	if inner, ok := ffigo.AsyncElemTypeString(typeName); ok {
		return r.NormalizeTypeString(inner)
	}
	if items, ok := ffigo.Tuple2ElemTypeStrings(typeName); ok {
		return string(miniast.CreateTupleType(
			miniast.GoMiniType(r.NormalizeTypeString(items[0])),
			miniast.GoMiniType(r.NormalizeTypeString(items[1])),
		))
	}
	miniType := miniast.GoMiniType(typeName)
	if elem, ok := miniType.GetPtrElementType(); ok {
		return string(miniast.GoMiniType(r.NormalizeTypeString(string(elem))).ToPtr())
	}
	if elem, ok := miniType.GetHostRefElementType(); ok {
		return string(miniast.GoMiniType(r.NormalizeTypeString(string(elem))).ToHostRef())
	}
	if elem, ok := miniType.ReadArrayItemType(); ok {
		return string(miniast.CreateArrayType(miniast.GoMiniType(r.NormalizeTypeString(string(elem)))))
	}
	if key, value, ok := miniType.GetMapKeyValueTypes(); ok {
		return string(miniast.CreateMapType(
			miniast.GoMiniType(r.NormalizeTypeString(string(key))),
			miniast.GoMiniType(r.NormalizeTypeString(string(value))),
		))
	}
	if tupleItems, ok := miniType.ReadTuple(); ok {
		items := make([]miniast.GoMiniType, 0, len(tupleItems))
		for _, item := range tupleItems {
			items = append(items, miniast.GoMiniType(r.NormalizeTypeString(string(item))))
		}
		return string(miniast.CreateTupleType(items...))
	}
	switch typeName {
	case "string":
		return "String"
	case "bool":
		return "Bool"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune":
		return "Int64"
	case "float32", "float64":
		return "Float64"
	case "[]byte":
		return "TypeBytes"
	case "error":
		return "Error"
	case ffigo.VoidQualifiedType, "ffigo.Void", "Void":
		return "Void"
	case "any", "interface{}":
		return "Any"
	case "context.Context", "Context":
		return "Context"
	}
	return r.displayName(typeName)
}

func (r *displayTypeResolver) VMType(expr ast.Expr) string {
	if r.gen.isBytesRefExpr(expr) {
		return "TypeBytes"
	}
	if inner, ok := r.gen.asyncElemExpr(expr); ok {
		return r.VMType(inner)
	}
	if items, ok := r.gen.tuple2ElemExprs(expr); ok {
		return string(miniast.CreateTupleType(
			miniast.GoMiniType(r.VMType(items[0])),
			miniast.GoMiniType(r.VMType(items[1])),
		))
	}
	if elemExpr, ok := r.gen.arrayRefElemExpr(expr); ok {
		return string(miniast.CreateArrayType(miniast.GoMiniType(r.VMType(elemExpr))))
	}
	if bt := r.gen.resolveToBasicType(expr); bt != "" {
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
		return string(miniast.CreateArrayType(miniast.GoMiniType(r.VMType(t.Elt))))
	case *ast.MapType:
		return string(miniast.CreateMapType(miniast.GoMiniType(r.VMType(t.Key)), miniast.GoMiniType(r.VMType(t.Value))))
	case *ast.StarExpr:
		return string(miniast.GoMiniType(r.VMType(t.X)).ToHostRef())
	case *ast.Ellipsis:
		return string(miniast.CreateArrayType(miniast.GoMiniType(r.VMType(t.Elt))))
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "Any"
		}
		return "Any"
	default:
		return r.NormalizeTypeString(r.gen.typeToString(expr))
	}
}

func (r *displayTypeResolver) displayName(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return "Any"
	}
	if strings.Contains(typeName, ".") {
		owner, name, ok := splitQualifiedTypeName(typeName)
		if ok {
			if knownPath, known := r.gen.knownImports[owner]; known {
				moduleName := r.gen.resolveImportedModule(knownPath)
				if moduleName == "" {
					return owner + "." + name
				}
				return moduleName + "." + name
			}
			if strings.Contains(owner, "/") {
				moduleName := r.gen.resolveImportedModule(owner)
				if moduleName == "" {
					for alias, path := range r.gen.knownImports {
						if path == owner {
							return alias + "." + name
						}
					}
					return typeName
				}
				return moduleName + "." + name
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

func structSchemaVarName(typeName string) string {
	replacer := strings.NewReplacer("/", "_", ".", "_", "<", "_", ">", "", ",", "_", " ", "_", "*", "_")
	return replacer.Replace(typeName) + "_FFI_StructSchema"
}

func interfaceSchemaVarName(typeName string) string {
	replacer := strings.NewReplacer("/", "_", ".", "_", "<", "_", ">", "", ",", "_", " ", "_", "*", "_")
	return replacer.Replace(typeName) + "_FFI_InterfaceSchema"
}

func buildInterfaceSchemaLiteral(iface *ast.InterfaceType, funcSpec func(*ast.FuncType) string) string {
	methods := make(map[string]*miniast.FunctionType)
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		fn, ok := miniast.GoMiniType(funcSpec(method.Type.(*ast.FuncType))).ReadFunc()
		if !ok {
			continue
		}
		methods[method.Names[0].Name] = fn
	}
	return string(miniast.CreateInterfaceType(methods))
}
