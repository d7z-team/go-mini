package ffigen

import (
	"fmt"
	"go/ast"
	"strings"
)

type referencedStructSet struct {
	ordered   []string
	ownership map[string]string
}

func collectReferencedStructs(iface *ast.InterfaceType, structs map[string]*ast.StructType, ownedStructs map[string]bool, currentOwned string) []string {
	return collectReferencedStructSet(iface, structs, ownedStructs, currentOwned).ordered
}

func collectReferencedStructSet(iface *ast.InterfaceType, structs map[string]*ast.StructType, ownedStructs map[string]bool, currentOwned string) referencedStructSet {
	res := referencedStructSet{ownership: make(map[string]string)}
	seen := make(map[string]bool)
	var visitType func(string, bool)
	visitType = func(typeName string, asHostRef bool) {
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			return
		}
		if strings.HasPrefix(typeName, "Ptr<") && strings.HasSuffix(typeName, ">") {
			visitType(typeName[4:len(typeName)-1], true)
			return
		}
		if strings.HasPrefix(typeName, "HostRef<") && strings.HasSuffix(typeName, ">") {
			visitType(typeName[8:len(typeName)-1], true)
			return
		}
		if strings.HasPrefix(typeName, "Array<") && strings.HasSuffix(typeName, ">") {
			visitType(typeName[6:len(typeName)-1], asHostRef)
			return
		}
		if strings.HasPrefix(typeName, "Map<") && strings.HasSuffix(typeName, ">") {
			inner := typeName[4 : len(typeName)-1]
			parts := strings.SplitN(inner, ",", 2)
			if len(parts) == 2 {
				visitType(strings.TrimSpace(parts[0]), asHostRef)
				visitType(strings.TrimSpace(parts[1]), asHostRef)
			}
			return
		}
		if strings.HasPrefix(typeName, "tuple(") && strings.HasSuffix(typeName, ")") {
			inner := typeName[6 : len(typeName)-1]
			for _, part := range strings.Split(inner, ",") {
				visitType(strings.TrimSpace(part), asHostRef)
			}
			return
		}
		if isPrimitive(typeName) || strings.HasPrefix(typeName, "interface{") {
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
				getFields(structs, localName, fieldMap)
				for _, fieldType := range fieldMap {
					visitType(fieldType, false)
				}
			}
		}
	}
	for _, method := range iface.Methods.List {
		if len(method.Names) == 0 {
			continue
		}
		funcType := method.Type.(*ast.FuncType)
		if funcType.Params != nil {
			for _, param := range funcType.Params.List {
				visitType(typeToString(param.Type), false)
			}
		}
		if funcType.Results != nil {
			for _, result := range funcType.Results.List {
				visitType(typeToString(result.Type), false)
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

func getFields(structs map[string]*ast.StructType, strName string, fieldMap map[string]string) {
	str, ok := structs[strName]
	if !ok {
		return
	}
	for _, f := range str.Fields.List {
		if len(f.Names) == 0 {
			tN := typeToString(f.Type)
			if inner, ok := refElementType(tN); ok {
				tN = inner
			}
			getFields(structs, tN, fieldMap)
		}
	}
	for _, f := range str.Fields.List {
		if len(f.Names) > 0 {
			for _, name := range f.Names {
				if ast.IsExported(name.Name) {
					fieldMap[name.Name] = typeToString(f.Type)
				}
			}
		}
	}
}
