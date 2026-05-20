package ffigen

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

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
		if obj := lookupTypeObject(t); obj != nil && obj.Pkg() != nil && obj.Pkg().Path() != packagePath {
			return obj.Pkg().Path() + "." + obj.Name()
		}
		return t.Name
	case *ast.ArrayType:
		return fmt.Sprintf("Array<%s>", typeToString(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", typeToString(t.Key), typeToString(t.Value))
	case *ast.StarExpr:
		return fmt.Sprintf("HostRef<%s>", typeToString(t.X))
	case *ast.SelectorExpr:
		if obj := lookupTypeObject(t.Sel); obj != nil && obj.Pkg() != nil {
			return obj.Pkg().Path() + "." + obj.Name()
		}
		if x, ok := t.X.(*ast.Ident); ok {
			if importPath, ok := knownImports[x.Name]; ok {
				return importPath + "." + t.Sel.Name
			}
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.IndexExpr:
		return fmt.Sprintf("%s<%s>", typeToString(t.X), typeToString(t.Index))
	case *ast.IndexListExpr:
		parts := make([]string, 0, len(t.Indices))
		for _, idx := range t.Indices {
			parts = append(parts, typeToString(idx))
		}
		return fmt.Sprintf("%s<%s>", typeToString(t.X), strings.Join(parts, ", "))
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

func lookupTypeObject(id *ast.Ident) types.Object {
	if id == nil || typeInfo == nil {
		return nil
	}
	if obj := typeInfo.Uses[id]; obj != nil {
		return obj
	}
	return typeInfo.Defs[id]
}

func toGoType(pType string) string {
	if inner, ok := refElementType(pType); ok {
		return "*" + toGoType(inner)
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
	if base, args, ok := splitGenericType(pType); ok {
		goArgs := make([]string, 0, len(args))
		for _, arg := range args {
			goArgs = append(goArgs, toGoType(arg))
		}
		return toGoType(base) + "[" + strings.Join(goArgs, ", ") + "]"
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
	case "Void":
		return "ffigo.Void"
	case "error":
		return "error"
	default:
		if owner, name, ok := splitQualifiedTypeName(pType); ok && strings.Contains(owner, "/") {
			for alias, path := range knownImports {
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

func isBytesRefExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	return isBytesRefTypeString(typeToString(expr))
}

func isArrayRefTypeString(typeName string) bool {
	return ffigo.IsArrayRefTypeString(typeName)
}

func isGenericTypeBase(typeName string, bases ...string) bool {
	return ffigo.IsGenericTypeBase(typeName, bases...)
}

func asyncElemTypeString(typeName string) (string, bool) {
	return ffigo.AsyncElemTypeString(typeName)
}

func tuple2ElemTypeStrings(typeName string) ([]string, bool) {
	return ffigo.Tuple2ElemTypeStrings(typeName)
}

func asyncElemExpr(expr ast.Expr) (ast.Expr, bool) {
	switch t := expr.(type) {
	case *ast.IndexExpr:
		if isGenericTypeBase(typeToString(t), ffigo.AsyncQualifiedType, "ffigo.Async", "Async") {
			return t.Index, true
		}
	case *ast.IndexListExpr:
		if isGenericTypeBase(typeToString(t), ffigo.AsyncQualifiedType, "ffigo.Async", "Async") && len(t.Indices) == 1 {
			return t.Indices[0], true
		}
	}
	return nil, false
}

func tuple2ElemExprs(expr ast.Expr) ([]ast.Expr, bool) {
	switch t := expr.(type) {
	case *ast.IndexListExpr:
		if isGenericTypeBase(typeToString(t), ffigo.Tuple2QualifiedType, "ffigo.Tuple2", "Tuple2") && len(t.Indices) == 2 {
			return t.Indices, true
		}
	}
	return nil, false
}

func arrayRefElemExpr(expr ast.Expr) (ast.Expr, bool) {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return nil, false
	}
	switch t := star.X.(type) {
	case *ast.IndexExpr:
		if isArrayRefTypeString(typeToString(t.X)) {
			return t.Index, true
		}
	case *ast.IndexListExpr:
		if isArrayRefTypeString(typeToString(t.X)) && len(t.Indices) == 1 {
			return t.Indices[0], true
		}
	}
	return nil, false
}

func copyBackKind(expr ast.Expr) string {
	switch {
	case isBytesRefExpr(expr):
		return "bytes"
	case func() bool {
		_, ok := arrayRefElemExpr(expr)
		return ok
	}():
		return "array"
	default:
		return ""
	}
}

func funcParamModes(funcType *ast.FuncType) []string {
	var modes []string
	if funcType == nil || funcType.Params == nil {
		return nil
	}
	for i, p := range funcType.Params.List {
		pType := typeToString(p.Type)
		if i == 0 && (pType == "context.Context" || pType == "Context") {
			continue
		}
		mode := "runtime.FFIParamIn"
		switch copyBackKind(p.Type) {
		case "bytes":
			mode = "runtime.FFIParamInOutBytes"
		case "array":
			mode = "runtime.FFIParamInOutArray"
		}
		count := len(p.Names)
		if count == 0 {
			count = 1
		}
		for j := 0; j < count; j++ {
			modes = append(modes, mode)
		}
	}
	return modes
}

func hasCopyBackParam(funcType *ast.FuncType) bool {
	for _, mode := range funcParamModes(funcType) {
		if mode == "runtime.FFIParamInOutBytes" || mode == "runtime.FFIParamInOutArray" {
			return true
		}
	}
	return false
}

func splitGenericType(typeName string) (string, []string, bool) {
	return ffigo.SplitGenericType(typeName)
}

func refElementType(typeName string) (string, bool) {
	return ffigo.RefElementType(typeName)
}

func isRefTypeString(typeName string) bool {
	return ffigo.IsRefTypeString(typeName)
}

func refTypeID(typeName, moduleName string) string {
	if inner, ok := refElementType(typeName); ok {
		inner = strings.TrimSpace(inner)
		if owner, name, ok := splitQualifiedTypeName(inner); ok {
			if knownPath, known := knownImports[owner]; known {
				if mod := resolveImportedModule(knownPath); mod != "" {
					return mod + "." + name
				}
				return owner + "." + name
			}
			if strings.Contains(owner, "/") {
				if mod := resolveImportedModule(owner); mod != "" {
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

func zeroValue(t string) string {
	if isRefTypeString(t) || strings.HasPrefix(t, "Array<") || t == "Any" || t == "any" || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") || t == "error" {
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
