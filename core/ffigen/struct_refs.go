package ffigen

import (
	"fmt"
	"go/ast"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type referencedStructSet struct {
	ordered   []string
	ownership map[string]string
}

func (g *Generator) collectReferencedStructSet(iface *ast.InterfaceType, structs map[string]*ast.StructType, ownedStructs map[string]bool, currentOwned string) referencedStructSet {
	res := referencedStructSet{ownership: make(map[string]string)}
	if iface == nil {
		return res
	}
	seen := make(map[string]bool)
	var visitType func(string, bool)
	visitType = func(typeName string, asHostRef bool) {
		walkNestedTypeNames(typeName, asHostRef, func(typeName string, asHostRef bool) {
			if isBuiltinScalarType(typeName) || isInterfaceTypeString(typeName) {
				return
			}
			localName := typeName
			if idx := strings.LastIndex(localName, "."); idx >= 0 {
				localName = localName[idx+1:]
			}
			if ownedStructs != nil && ownedStructs[localName] && localName != currentOwned {
				return
			}
			if structs[localName] != nil {
				ownership := "StructOwnershipVMValue"
				if asHostRef {
					ownership = "StructOwnershipHostOpaque"
				}
				if existing, ok := res.ownership[localName]; ok && existing != ownership {
					panic(fmt.Sprintf("ffigen: type %s is used both as VM value and host opaque reference", localName))
				}
				res.ownership[localName] = ownership
			}
			if !seen[localName] && structs[localName] != nil {
				seen[localName] = true
				res.ordered = append(res.ordered, localName)
				if !asHostRef {
					fieldMap := make(map[string]string)
					g.getFields(structs, localName, fieldMap)
					for _, fieldType := range fieldMap {
						visitType(fieldType, false)
					}
				}
			}
		})
	}
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		funcType := method.Type.(*ast.FuncType)
		if funcType.Params != nil {
			for _, param := range funcType.Params.List {
				visitType(g.typeToString(param.Type), false)
			}
		}
		if funcType.Results != nil {
			for _, result := range funcType.Results.List {
				visitType(g.typeToString(result.Type), false)
			}
		}
	}
	return res
}

func generatedIdentSuffix(raw string) string {
	var b strings.Builder
	for i, r := range raw {
		if r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || (i > 0 && '0' <= r && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

func (g *Generator) getFields(structs map[string]*ast.StructType, strName string, fieldMap map[string]string) {
	str, ok := structs[strName]
	if !ok {
		return
	}
	for _, f := range str.Fields.List {
		if len(f.Names) == 0 {
			tN := g.typeToString(f.Type)
			if inner, ok := ffigo.RefElementType(tN); ok {
				tN = inner
			}
			g.getFields(structs, tN, fieldMap)
		}
	}
	for _, f := range str.Fields.List {
		if len(f.Names) > 0 {
			for _, name := range f.Names {
				if ast.IsExported(name.Name) {
					fieldMap[name.Name] = g.typeToString(f.Type)
				}
			}
		}
	}
}
