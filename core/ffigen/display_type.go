package ffigen

import (
	"fmt"
	"go/ast"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func newDisplayTypeResolver(moduleName string, iface *ast.InterfaceType, structs map[string]*ast.StructType, methodsPrefix string, ownedStructs map[string]bool, currentOwned string) *displayTypeResolver {
	resolver := &displayTypeResolver{
		moduleName:        moduleName,
		importAliases:     make(map[string]string, len(knownImports)),
		collidingBaseName: make(map[string]bool),
	}
	for alias, path := range knownImports {
		resolver.importAliases[alias] = path
	}
	nameOwners := make(map[string]string)
	record := func(typeName string) {
		for _, named := range collectNamedTypeRefs(typeName) {
			baseName := named
			if idx := strings.LastIndex(baseName, "."); idx >= 0 {
				baseName = baseName[idx+1:]
			}
			owner := named
			if idx := strings.Index(owner, "."); idx >= 0 {
				owner = owner[:idx]
			}
			if previous, ok := nameOwners[baseName]; ok && previous != owner {
				resolver.collidingBaseName[baseName] = true
				continue
			}
			nameOwners[baseName] = owner
		}
	}
	record(methodsPrefix)
	if iface != nil {
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				continue
			}
			funcType := method.Type.(*ast.FuncType)
			if funcType.Params != nil {
				for _, param := range funcType.Params.List {
					record(typeToString(param.Type))
				}
			}
			if funcType.Results != nil {
				for _, result := range funcType.Results.List {
					record(typeToString(result.Type))
				}
			}
		}
	}
	for _, structName := range collectReferencedStructs(iface, structs, ownedStructs, currentOwned) {
		fieldMap := make(map[string]string)
		getFields(structs, structName, fieldMap)
		for _, fieldType := range fieldMap {
			record(fieldType)
		}
	}
	return resolver
}

func collectNamedTypeRefs(typeName string) []string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil
	}
	if strings.HasPrefix(typeName, "Ptr<") && strings.HasSuffix(typeName, ">") {
		return collectNamedTypeRefs(typeName[4 : len(typeName)-1])
	}
	if strings.HasPrefix(typeName, "HostRef<") && strings.HasSuffix(typeName, ">") {
		return collectNamedTypeRefs(typeName[8 : len(typeName)-1])
	}
	if strings.HasPrefix(typeName, "Array<") && strings.HasSuffix(typeName, ">") {
		return collectNamedTypeRefs(typeName[6 : len(typeName)-1])
	}
	if strings.HasPrefix(typeName, "Map<") && strings.HasSuffix(typeName, ">") {
		inner := typeName[4 : len(typeName)-1]
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) != 2 {
			return nil
		}
		return append(
			collectNamedTypeRefs(strings.TrimSpace(parts[0])),
			collectNamedTypeRefs(strings.TrimSpace(parts[1]))...,
		)
	}
	if strings.HasPrefix(typeName, "tuple(") && strings.HasSuffix(typeName, ")") {
		var refs []string
		for _, part := range strings.Split(typeName[6:len(typeName)-1], ",") {
			refs = append(refs, collectNamedTypeRefs(strings.TrimSpace(part))...)
		}
		return refs
	}
	if isPrimitive(typeName) || typeName == "error" || typeName == "any" || typeName == "interface{}" || typeName == "context.Context" || typeName == "Context" {
		return nil
	}
	return []string{typeName}
}

func (r *displayTypeResolver) NormalizeTypeString(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return "Any"
	}
	if inner, ok := asyncElemTypeString(typeName); ok {
		return r.NormalizeTypeString(inner)
	}
	if items, ok := tuple2ElemTypeStrings(typeName); ok {
		return "tuple(" + r.NormalizeTypeString(items[0]) + ", " + r.NormalizeTypeString(items[1]) + ")"
	}
	if strings.HasPrefix(typeName, "Ptr<") && strings.HasSuffix(typeName, ">") {
		return "Ptr<" + r.NormalizeTypeString(typeName[4:len(typeName)-1]) + ">"
	}
	if strings.HasPrefix(typeName, "HostRef<") && strings.HasSuffix(typeName, ">") {
		return "HostRef<" + r.NormalizeTypeString(typeName[8:len(typeName)-1]) + ">"
	}
	if strings.HasPrefix(typeName, "Array<") && strings.HasSuffix(typeName, ">") {
		return "Array<" + r.NormalizeTypeString(typeName[6:len(typeName)-1]) + ">"
	}
	if strings.HasPrefix(typeName, "Map<") && strings.HasSuffix(typeName, ">") {
		inner := typeName[4 : len(typeName)-1]
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("Map<%s, %s>", r.NormalizeTypeString(strings.TrimSpace(parts[0])), r.NormalizeTypeString(strings.TrimSpace(parts[1])))
		}
	}
	if strings.HasPrefix(typeName, "tuple(") && strings.HasSuffix(typeName, ")") {
		var normalized []string
		for _, part := range strings.Split(typeName[6:len(typeName)-1], ",") {
			normalized = append(normalized, r.NormalizeTypeString(strings.TrimSpace(part)))
		}
		return "tuple(" + strings.Join(normalized, ", ") + ")"
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
	if isBytesRefExpr(expr) {
		return "TypeBytes"
	}
	if inner, ok := asyncElemExpr(expr); ok {
		return r.VMType(inner)
	}
	if items, ok := tuple2ElemExprs(expr); ok {
		return "tuple(" + r.VMType(items[0]) + ", " + r.VMType(items[1]) + ")"
	}
	if elemExpr, ok := arrayRefElemExpr(expr); ok {
		return fmt.Sprintf("Array<%s>", r.VMType(elemExpr))
	}
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
	case *ast.ArrayType:
		if ident, ok := t.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
			return "TypeBytes"
		}
		return fmt.Sprintf("Array<%s>", r.VMType(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", r.VMType(t.Key), r.VMType(t.Value))
	case *ast.StarExpr:
		return fmt.Sprintf("HostRef<%s>", r.VMType(t.X))
	case *ast.Ellipsis:
		return fmt.Sprintf("Array<%s>", r.VMType(t.Elt))
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "Any"
		}
		return "Any"
	default:
		return r.NormalizeTypeString(typeToString(expr))
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
			if knownPath, known := knownImports[owner]; known {
				moduleName := resolveImportedModule(knownPath)
				if moduleName == "" {
					return owner + "." + name
				}
				return moduleName + "." + name
			}
			if strings.Contains(owner, "/") {
				moduleName := resolveImportedModule(owner)
				if moduleName == "" {
					for alias, path := range knownImports {
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
	var parts []string
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		suffix := strings.TrimPrefix(funcSpec(method.Type.(*ast.FuncType)), "function")
		parts = append(parts, method.Names[0].Name+suffix+";")
	}
	return "interface{" + strings.Join(parts, "") + "}"
}
