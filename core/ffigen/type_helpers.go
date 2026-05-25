package ffigen

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func isBuiltinScalarType(name string) bool {
	switch name {
	case "Int64", "Float64", "String", "Bool", "Any", "Error", "Void", "TypeBytes",
		"int", "int8", "int16", "int32", "int64", "rune",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte",
		"float32", "float64", "string", "bool", "any", "error":
		return true
	}
	return false
}

func (g *Generator) typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		if obj := g.lookupTypeObject(t); obj != nil && obj.Pkg() != nil && obj.Pkg().Path() != g.packagePath {
			return obj.Pkg().Path() + "." + obj.Name()
		}
		return t.Name
	case *ast.ArrayType:
		return string(miniast.CreateArrayType(miniast.GoMiniType(g.typeToString(t.Elt))))
	case *ast.MapType:
		return string(miniast.CreateMapType(
			miniast.GoMiniType(g.typeToString(t.Key)),
			miniast.GoMiniType(g.typeToString(t.Value)),
		))
	case *ast.ChanType:
		elem := miniast.GoMiniType(g.typeToString(t.Value))
		switch t.Dir {
		case ast.RECV:
			return string(miniast.CreateRecvChanType(elem))
		case ast.SEND:
			return string(miniast.CreateSendChanType(elem))
		default:
			return string(miniast.CreateChanType(elem))
		}
	case *ast.StarExpr:
		return string(miniast.GoMiniType(g.typeToString(t.X)).ToHostRef())
	case *ast.SelectorExpr:
		if obj := g.lookupTypeObject(t.Sel); obj != nil && obj.Pkg() != nil {
			return obj.Pkg().Path() + "." + obj.Name()
		}
		if x, ok := t.X.(*ast.Ident); ok {
			if importPath, ok := g.knownImports[x.Name]; ok {
				return importPath + "." + t.Sel.Name
			}
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.IndexExpr:
		return fmt.Sprintf("%s<%s>", g.typeToString(t.X), g.typeToString(t.Index))
	case *ast.IndexListExpr:
		parts := make([]string, 0, len(t.Indices))
		for _, idx := range t.Indices {
			parts = append(parts, g.typeToString(idx))
		}
		return fmt.Sprintf("%s<%s>", g.typeToString(t.X), strings.Join(parts, ", "))
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "Any"
		}
		return "interface{}"
	case *ast.StructType:
		if t.Fields == nil || len(t.Fields.List) == 0 {
			return "Void"
		}
		return "Any"
	case *ast.Ellipsis:
		return string(miniast.CreateArrayType(miniast.GoMiniType(g.typeToString(t.Elt))))
	default:
		return "Any"
	}
}

func (g *Generator) lookupTypeObject(id *ast.Ident) types.Object {
	if id == nil || g.typeInfo == nil {
		return nil
	}
	if obj := g.typeInfo.Uses[id]; obj != nil {
		return obj
	}
	return g.typeInfo.Defs[id]
}

func (g *Generator) unsupportedInterfaceExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	if t, ok := expr.(*ast.InterfaceType); ok {
		return t.Methods != nil && len(t.Methods.List) > 0
	}
	if g.typeInfo == nil {
		return false
	}
	tv, ok := g.typeInfo.Types[expr]
	if !ok || tv.Type == nil {
		return false
	}
	iface, ok := tv.Type.Underlying().(*types.Interface)
	return ok && iface.NumMethods() > 0
}

func (g *Generator) toGoType(pType string) string {
	if inner, ok := ffigo.RefElementType(pType); ok {
		return "*" + g.toGoType(inner)
	}
	miniType := miniast.GoMiniType(pType)
	if elem, ok := miniType.ReadChanElemType(); ok {
		goElem := g.toGoType(string(elem))
		switch {
		case miniType.IsRecvChan():
			return "<-chan " + goElem
		case miniType.IsSendChan():
			return "chan<- " + goElem
		default:
			return "chan " + goElem
		}
	}
	if innerType, ok := miniType.ReadArrayItemType(); ok {
		inner := string(innerType)
		if inner == "Uint8" || inner == "byte" || inner == "uint8" {
			return "[]byte"
		}
		return "[]" + g.toGoType(inner)
	}
	if key, value, ok := miniType.GetMapKeyValueTypes(); ok {
		return "map[" + g.toGoType(string(key)) + "]" + g.toGoType(string(value))
	}
	if base, args, ok := ffigo.SplitGenericType(pType); ok {
		goArgs := make([]string, 0, len(args))
		for _, arg := range args {
			goArgs = append(goArgs, g.toGoType(arg))
		}
		return g.toGoType(base) + "[" + strings.Join(goArgs, ", ") + "]"
	}
	switch pType {
	case "uint64":
		return "uint64"
	case "uint32":
		return "uint32"
	case "uint16":
		return "uint16"
	case "byte", "uint8":
		return "uint8"
	case "uintptr":
		return "uintptr"
	case "int":
		return "int"
	case "Int64", "int64":
		return "int64"
	case "int32":
		return "int32"
	case "int16":
		return "int16"
	case "int8":
		return "int8"
	case "rune":
		return "rune"
	case "uint":
		return "uint"
	case "String", "string":
		return "string"
	case "Bool", "bool":
		return "bool"
	case "Float64", "float64":
		return "float64"
	case "float32":
		return "float32"
	case "context.Context", "Context":
		return "context.Context"
	case "Any", "any", "interface{}":
		return "any"
	case "TypeBytes":
		return "[]byte"
	case "Void":
		return "ffigo.Void"
	case "error":
		return "error"
	default:
		if owner, name, ok := splitQualifiedTypeName(pType); ok && strings.Contains(owner, "/") {
			for alias, path := range g.knownImports {
				if path == owner {
					return alias + "." + name
				}
			}
			parts := strings.Split(owner, "/")
			return parts[len(parts)-1] + "." + name
		}
		return pType
	}
}

func isBytesRefTypeString(typeName string) bool {
	return ffigo.IsBytesRefTypeString(typeName)
}

func (g *Generator) isBytesRefExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	return isBytesRefTypeString(g.typeToString(expr))
}

func isArrayRefTypeString(typeName string) bool {
	return ffigo.IsArrayRefTypeString(typeName)
}

func isGenericTypeBase(typeName string, bases ...string) bool {
	return ffigo.IsGenericTypeBase(typeName, bases...)
}

func (g *Generator) asyncElemExpr(expr ast.Expr) (ast.Expr, bool) {
	switch t := expr.(type) {
	case *ast.IndexExpr:
		if isGenericTypeBase(g.typeToString(t), ffigo.AsyncQualifiedType, "ffigo.Async", "Async") {
			return t.Index, true
		}
	case *ast.IndexListExpr:
		if isGenericTypeBase(g.typeToString(t), ffigo.AsyncQualifiedType, "ffigo.Async", "Async") && len(t.Indices) == 1 {
			return t.Indices[0], true
		}
	}
	return nil, false
}

func (g *Generator) tuple2ElemExprs(expr ast.Expr) ([]ast.Expr, bool) {
	switch t := expr.(type) {
	case *ast.IndexListExpr:
		if isGenericTypeBase(g.typeToString(t), ffigo.Tuple2QualifiedType, "ffigo.Tuple2", "Tuple2") && len(t.Indices) == 2 {
			return t.Indices, true
		}
	}
	return nil, false
}

func (g *Generator) arrayRefElemExpr(expr ast.Expr) (ast.Expr, bool) {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return nil, false
	}
	switch t := star.X.(type) {
	case *ast.IndexExpr:
		if isArrayRefTypeString(g.typeToString(t.X)) {
			return t.Index, true
		}
	case *ast.IndexListExpr:
		if isArrayRefTypeString(g.typeToString(t.X)) && len(t.Indices) == 1 {
			return t.Indices[0], true
		}
	}
	return nil, false
}

func (g *Generator) copyBackKind(expr ast.Expr) string {
	switch {
	case g.isBytesRefExpr(expr):
		return "bytes"
	case func() bool {
		_, ok := g.arrayRefElemExpr(expr)
		return ok
	}():
		return "array"
	default:
		return ""
	}
}

func isRefTypeString(typeName string) bool {
	return ffigo.IsRefTypeString(typeName)
}

func isInterfaceTypeString(typeName string) bool {
	typeName = strings.TrimSpace(typeName)
	return miniast.GoMiniType(typeName).IsInterface() || strings.HasPrefix(typeName, "interface{")
}

func walkNestedTypeNames(typeName string, asHostRef bool, visit func(string, bool)) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || visit == nil {
		return
	}
	miniType := miniast.GoMiniType(typeName)
	if elem, ok := miniType.GetPtrElementType(); ok {
		walkNestedTypeNames(string(elem), true, visit)
		return
	}
	if elem, ok := miniType.GetHostRefElementType(); ok {
		walkNestedTypeNames(string(elem), true, visit)
		return
	}
	if elem, ok := miniType.ReadArrayItemType(); ok {
		walkNestedTypeNames(string(elem), asHostRef, visit)
		return
	}
	if key, value, ok := miniType.GetMapKeyValueTypes(); ok {
		walkNestedTypeNames(string(key), asHostRef, visit)
		walkNestedTypeNames(string(value), asHostRef, visit)
		return
	}
	if elem, ok := miniType.ReadChanElemType(); ok {
		walkNestedTypeNames(string(elem), asHostRef, visit)
		return
	}
	if items, ok := miniType.ReadTuple(); ok {
		for _, item := range items {
			walkNestedTypeNames(string(item), asHostRef, visit)
		}
		return
	}
	visit(typeName, asHostRef)
}

func (g *Generator) refTypeID(typeName, moduleName string) string {
	if inner, ok := ffigo.RefElementType(typeName); ok {
		inner = strings.TrimSpace(inner)
		if owner, name, ok := splitQualifiedTypeName(inner); ok {
			if knownPath, known := g.knownImports[owner]; known {
				if mod := g.resolveImportedModule(knownPath); mod != "" {
					return mod + "." + name
				}
				return owner + "." + name
			}
			if strings.Contains(owner, "/") {
				if mod := g.resolveImportedModule(owner); mod != "" {
					return mod + "." + name
				}
			}
			return inner
		}
		if moduleName != "" {
			return moduleName + "." + inner
		}
		return inner
	}
	return ""
}

func readArrayItemType(pType string) (string, bool) {
	return ffigo.ReadArrayItemType(pType)
}

func readMapKeyValueTypes(pType string) (string, string, bool) {
	return ffigo.ReadMapKeyValueTypes(pType)
}

func readChanElemType(pType string) (string, bool) {
	return ffigo.ReadChanElemType(pType)
}

func channelDirectionLiteral(pType string) string {
	pType = strings.TrimSpace(pType)
	switch {
	case strings.HasPrefix(pType, "RecvChan<"):
		return "ffigo.ChannelRecv"
	case strings.HasPrefix(pType, "SendChan<"):
		return "ffigo.ChannelSend"
	default:
		return "ffigo.ChannelBoth"
	}
}

func channelTypeCanRecv(pType string) bool {
	pType = strings.TrimSpace(pType)
	return !strings.HasPrefix(pType, "SendChan<")
}

func channelTypeCanSend(pType string) bool {
	pType = strings.TrimSpace(pType)
	return !strings.HasPrefix(pType, "RecvChan<")
}

func isBidirectionalChannelType(pType string) bool {
	miniType := miniast.GoMiniType(pType)
	return miniType.IsChan() && !miniType.IsRecvChan() && !miniType.IsSendChan()
}

func zeroValue(t string) string {
	miniType := miniast.GoMiniType(t)
	if isRefTypeString(t) || miniType.IsArray() || miniType.IsMap() || miniType.IsChan() || miniType.IsAny() || t == "any" || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") || t == "error" {
		return "nil"
	}
	switch t {
	case "int", "int8", "int16", "int32", "Int64", "int64", "rune",
		"uint", "uint8", "byte", "uint16", "uint32", "uint64", "uintptr":
		return "0"
	case "String", "string":
		return "\"\""
	case "Bool", "bool":
		return "false"
	case "float32", "Float64", "float64":
		return "0.0"
	case "TypeBytes":
		return "nil"
	default:
		return t + "{}"
	}
}
