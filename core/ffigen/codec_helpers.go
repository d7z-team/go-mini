package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

func (g *Generator) emitWrite(sb *strings.Builder, prefix, pType string, expr ast.Expr, structs map[string]*ast.StructType, bufName, moduleName string, isHost bool) {
	if isBytesRefTypeString(pType) {
		fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteBytes(nil)\n\t} else {\n\t\t%s.WriteBytes(%s.Value)\n\t}\n", prefix, bufName, bufName, prefix)
		return
	}
	if isRefTypeString(pType) {
		typeID := g.refTypeID(pType, moduleName)
		fmt.Fprintf(sb, "\t// HostRef<T> crosses the FFI boundary as an opaque handle ID.\n")
		fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteUvarint(0)\n\t} else {\n", prefix, bufName)
		if isHost {
			fmt.Fprintf(sb, "\t\t%s.WriteUvarint(uint64(registry.RegisterTyped(%s, \"%s\")))\n", bufName, prefix, typeID)
		} else {
			fmt.Fprintf(sb, "\t\tif __p.registry != nil { %s.WriteUvarint(uint64(__p.registry.RegisterTyped(%s, \"%s\"))) } else { %s.WriteUvarint(0) }\n", bufName, prefix, typeID, bufName)
		}

		fmt.Fprintf(sb, "\t}\n")
		return
	}
	bt := codecBasicType(g.resolveToBasicType(expr))
	if bt == "" {
		bt = codecBasicType(pType)
	}

	if bt != "" {
		switch {
		case codecSignedBasic(bt):
			fmt.Fprintf(sb, "\t%s.WriteVarint(int64(%s))\n", bufName, prefix)
			return
		case codecUnsignedBasic(bt):
			fmt.Fprintf(sb, "\t%s.WriteVarint(int64(%s))\n", bufName, prefix)
			return
		case strings.HasPrefix(bt, "float"):
			fmt.Fprintf(sb, "\t%s.WriteFloat64(float64(%s))\n", bufName, prefix)
			return
		case bt == "string":
			fmt.Fprintf(sb, "\t%s.WriteString(string(%s))\n", bufName, prefix)
			return
		case bt == "bool":
			fmt.Fprintf(sb, "\t%s.WriteBool(bool(%s))\n", bufName, prefix)
			return
		}
	}

	switch pType {
	case "[]byte", "TypeBytes", "Array<Uint8>", "Array<byte>":
		fmt.Fprintf(sb, "\t%s.WriteBytes(%s)\n", bufName, prefix)
	case "Any", "any":
		fmt.Fprintf(sb, "\t%s.WriteAny(%s)\n", bufName, prefix)
	default:
		if itemType, ok := readArrayItemType(pType); ok {
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(len(%s)))\n\tfor _, item := range %s {\n", bufName, prefix, prefix)
			g.emitWrite(sb, "item", itemType, nil, structs, bufName, moduleName, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(pType); ok {
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(len(%s)))\n\tfor k, v := range %s {\n", bufName, prefix, prefix)
			g.emitWrite(sb, "k", kType, nil, structs, bufName, moduleName, isHost)
			g.emitWrite(sb, "v", vType, nil, structs, bufName, moduleName, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if _, ok := structs[pType]; ok {
			fMap := make(map[string]string)
			g.getFields(structs, pType, fMap)
			var fNames []string
			for fn := range fMap {
				fNames = append(fNames, fn)
			}
			sort.Strings(fNames)
			for _, fn := range fNames {
				g.emitWrite(sb, prefix+"."+fn, fMap[fn], nil, structs, bufName, moduleName, isHost)
			}
		} else {
			unsupportedBareFFIType(pType)
		}
	}
}

func (g *Generator) emitReadAssign(sb *strings.Builder, varName, pType string, expr ast.Expr, structs map[string]*ast.StructType, readerName, moduleName string, isHost bool) {
	if isBytesRefTypeString(pType) {
		fmt.Fprintf(sb, "\t%s = &ffigo.BytesRef{Value: %s.ReadBytes()}\n", varName, readerName)
		return
	}
	if isRefTypeString(pType) {
		typeID := g.refTypeID(pType, moduleName)
		fmt.Fprintf(sb, "\t// HostRef<T> is restored from the opaque handle ID written on the FFI wire.\n")
		if isHost {
			fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if obj, err := registry.GetTypedWithAudit(id, \"%s\"); err == nil { %s = obj.(%s) } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) } }\n", readerName, typeID, varName, g.toGoType(pType), varName)
		} else {
			fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if __p.registry != nil { if obj, ok := __p.registry.GetTyped(id, \"%s\"); ok { %s = obj.(%s) } } }\n", readerName, typeID, varName, g.toGoType(pType))
		}
		return
	}
	bt := codecBasicType(g.resolveToBasicType(expr))
	if bt == "" {
		bt = codecBasicType(pType)
	}

	if bt != "" {
		gt := g.toGoType(pType)
		switch {
		case codecSignedBasic(bt):
			fmt.Fprintf(sb, "\t{\n\ttmp := %s.ReadVarint()\n", readerName)
			switch bt {
			case "int8":
				fmt.Fprintf(sb, "\tif tmp < -128 || tmp > 127 { panic(fmt.Sprintf(\"ffi: int8 overflow: %%d\", tmp)) }\n")
			case "int16":
				fmt.Fprintf(sb, "\tif tmp < -32768 || tmp > 32767 { panic(fmt.Sprintf(\"ffi: int16 overflow: %%d\", tmp)) }\n")
			case "int32":
				fmt.Fprintf(sb, "\tif tmp < -2147483648 || tmp > 2147483647 { panic(fmt.Sprintf(\"ffi: int32 overflow: %%d\", tmp)) }\n")
			case "int":
				// Go's int is at least 32 bits; generated bindings assume a 64-bit VM integer.
			}
			fmt.Fprintf(sb, "\t%s = %s(tmp)\n\t}\n", varName, gt)
			return
		case codecUnsignedBasic(bt):
			fmt.Fprintf(sb, "\t{\n\ttmp := %s.ReadVarint()\n", readerName)
			if maxLiteral := codecUnsignedMaxLiteral(bt); maxLiteral != "" {
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > %s { panic(fmt.Sprintf(\"ffi: %s overflow: %%d\", tmp)) }\n", maxLiteral, bt)
			} else if isHost {
				fmt.Fprintf(sb, "\tif tmp < 0 { panic(fmt.Sprintf(\"ffi: %s overflow: %%d\", tmp)) }\n", bt)
			}
			fmt.Fprintf(sb, "\t%s = %s(tmp)\n\t}\n", varName, gt)
			return
		case strings.HasPrefix(bt, "float"):
			fmt.Fprintf(sb, "\t%s = %s(%s.ReadFloat64())\n", varName, gt, readerName)
			return
		case bt == "string":
			fmt.Fprintf(sb, "\t%s = %s(%s.ReadString())\n", varName, gt, readerName)
			return
		case bt == "bool":
			fmt.Fprintf(sb, "\t%s = %s(%s.ReadBool())\n", varName, gt, readerName)
			return
		}
	}

	switch pType {
	case "[]byte", "TypeBytes", "Array<Uint8>", "Array<byte>":
		fmt.Fprintf(sb, "\t%s = %s.ReadBytes()\n", varName, readerName)
	case "bool", "Bool":
		fmt.Fprintf(sb, "\t%s = %s.ReadBool()\n", varName, readerName)
	case "float64", "Float64", "float32", "Float32":
		fmt.Fprintf(sb, "\t%s = %s.ReadFloat64()\n", varName, readerName)
	case "Any", "any":
		if isHost {
			fmt.Fprintf(sb, "\trawVal = %s.ReadAny()\n\tswitch rv := rawVal.(type) {\n\tcase uint32: if obj, err := registry.GetWithAudit(rv); err == nil { %s = obj } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) }\n\tcase ffigo.ErrorData: if rv.Handle != 0 { if obj, err := registry.GetWithAudit(rv.Handle); err == nil { %s = obj } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) } } else { %s = rv }\n\tdefault: %s = rawVal\n\t}\n", readerName, varName, varName, varName, varName, varName, varName)
		} else {
			fmt.Fprintf(sb, "\t%s = %s.ReadAny()\n", varName, readerName)
		}
	default:
		if itemType, ok := readArrayItemType(pType); ok {
			suffix := generatedIdentSuffix(varName)
			lenVar := "l_" + suffix
			idxVar := "i_" + suffix
			fmt.Fprintf(sb, "\t%s := int(%s.ReadUvarint())\n\t%s = make(%s, %s)\n\tfor %s := 0; %s < %s; %s++ {\n", lenVar, readerName, varName, g.toGoType(pType), lenVar, idxVar, idxVar, lenVar, idxVar)
			g.emitReadAssign(sb, fmt.Sprintf("%s[%s]", varName, idxVar), itemType, nil, structs, readerName, moduleName, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(pType); ok {
			suffix := generatedIdentSuffix(varName)
			lenVar := "l_" + suffix
			idxVar := "i_" + suffix
			fmt.Fprintf(sb, "\t%s := int(%s.ReadUvarint())\n\t%s = make(%s)\n\tfor %s := 0; %s < %s; %s++ {\n\t\tvar k %s\n\t\tvar v %s\n", lenVar, readerName, varName, g.toGoType(pType), idxVar, idxVar, lenVar, idxVar, g.toGoType(kType), g.toGoType(vType))
			g.emitReadAssign(sb, "k", kType, nil, structs, readerName, moduleName, isHost)
			g.emitReadAssign(sb, "v", vType, nil, structs, readerName, moduleName, isHost)
			fmt.Fprintf(sb, "\t\t%s[k] = v\n\t}\n", varName)
			return
		}
		if _, ok := structs[pType]; ok {
			fMap := make(map[string]string)
			g.getFields(structs, pType, fMap)
			var fNames []string
			for fn := range fMap {
				fNames = append(fNames, fn)
			}
			sort.Strings(fNames)
			for _, fn := range fNames {
				g.emitReadAssign(sb, varName+"."+fn, fMap[fn], nil, structs, readerName, moduleName, isHost)
			}
		} else {
			unsupportedBareFFIType(pType)
		}
	}
}

func codecBasicType(typeName string) string {
	switch strings.TrimSpace(typeName) {
	case "int":
		return "int"
	case "int8":
		return "int8"
	case "int16":
		return "int16"
	case "int32", "rune":
		return "int32"
	case "Int64", "int64":
		return "int64"
	case "uint":
		return "uint"
	case "uint8", "byte":
		return "uint8"
	case "uint16":
		return "uint16"
	case "uint32":
		return "uint32"
	case "uint64":
		return "uint64"
	case "uintptr":
		return "uintptr"
	case "Float64", "float64":
		return "float64"
	case "float32":
		return "float32"
	case "String", "string":
		return "string"
	case "Bool", "bool":
		return "bool"
	default:
		return ""
	}
}

func codecSignedBasic(typeName string) bool {
	switch typeName {
	case "int", "int8", "int16", "int32", "int64":
		return true
	default:
		return false
	}
}

func codecUnsignedBasic(typeName string) bool {
	switch typeName {
	case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return true
	default:
		return false
	}
}

func codecUnsignedMaxLiteral(typeName string) string {
	switch typeName {
	case "uint8":
		return "255"
	case "uint16":
		return "65535"
	case "uint32":
		return "4294967295"
	default:
		return ""
	}
}

func unsupportedBareFFIType(typeName string) {
	panic(fmt.Sprintf("ffigen: unsupported bare FFI type %q; use *T/HostRef<T> for host objects or declare a local ffigen struct schema", typeName))
}
