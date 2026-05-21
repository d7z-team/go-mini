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
	if isInterfaceTypeString(pType) {
		fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteRawInterface(0, nil)\n\t} else {\n\t\tmethods := make(map[string]string)\n", prefix, bufName)
		if isHost {
			fmt.Fprintf(sb, "\t\t%s.WriteRawInterface(registry.Register(%s), methods)\n", bufName, prefix)
		} else {
			fmt.Fprintf(sb, "\t\tif __p.registry != nil { %s.WriteRawInterface(__p.registry.Register(%s), methods) } else { %s.WriteRawInterface(0, nil) }\n", bufName, prefix, bufName)
		}
		fmt.Fprintf(sb, "\t}\n")
		return
	}

	bt := g.resolveToBasicType(expr)
	if bt == "" {
		switch pType {
		case "int", "int8", "int16", "int32", "int64", "Int", "Int8", "Int16", "Int32", "Int64":
			bt = "int64"
		case "uint", "uint8", "uint16", "uint32", "uint64", "Uint", "Uint8", "Uint16", "Uint32", "Uint64", "byte":
			bt = "uint64"
		case "float32", "float64", "Float32", "Float64":
			bt = "float64"
		case "string", "String":
			bt = "string"
		case "bool", "Bool":
			bt = "bool"
		}
	}

	if bt != "" {
		switch {
		case strings.HasPrefix(bt, "int"):
			fmt.Fprintf(sb, "\t%s.WriteVarint(int64(%s))\n", bufName, prefix)
			return
		case strings.HasPrefix(bt, "uint") || bt == "byte":
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(%s))\n", bufName, prefix)
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
			fmt.Fprintf(sb, "\t// Treating %s as Handle\n", pType)
			gt := g.toGoType(pType)
			isN := strings.HasPrefix(gt, "*") || strings.HasPrefix(gt, "[]") || strings.HasPrefix(gt, "map[") || gt == "any" || gt == "error"
			if isN {
				fmt.Fprintf(sb, "\tif %s == nil { %s.WriteUvarint(0) } else {\n", prefix, bufName)
			}
			if isHost {
				fmt.Fprintf(sb, "\t\t%s.WriteUvarint(uint64(registry.Register(%s)))\n", bufName, prefix)
			} else {
				fmt.Fprintf(sb, "\t\tif __p.registry != nil { %s.WriteUvarint(uint64(__p.registry.Register(%s))) } else { %s.WriteUvarint(0) }\n", bufName, prefix, bufName)
			}
			if isN {
				fmt.Fprintf(sb, "\t}\n")
			}
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
	if isInterfaceTypeString(pType) {
		fmt.Fprintf(sb, "\tif idat := %s.ReadRawInterface(); idat.Handle != 0 { _ = idat }\n", readerName)
		return
	}

	bt := g.resolveToBasicType(expr)
	if bt == "" {
		switch pType {
		case "int", "Int":
			bt = "int"
		case "int8", "Int8":
			bt = "int8"
		case "int16", "Int16":
			bt = "int16"
		case "int32", "Int32":
			bt = "int32"
		case "int64", "Int64":
			bt = "int64"
		case "uint", "Uint":
			bt = "uint"
		case "uint8", "Uint8", "byte":
			bt = "uint8"
		case "uint16", "Uint16":
			bt = "uint16"
		case "uint32", "Uint32":
			bt = "uint32"
		case "uint64", "Uint64":
			bt = "uint64"
		case "float32", "Float32":
			bt = "float32"
		case "float64", "Float64":
			bt = "float64"
		case "string", "String":
			bt = "string"
		case "bool", "Bool":
			bt = "bool"
		}
	}

	if bt != "" {
		gt := g.toGoType(pType)
		switch {
		case strings.HasPrefix(bt, "int") || strings.HasPrefix(bt, "uint") || bt == "byte":
			fmt.Fprintf(sb, "\t{\n\ttmp := %s.ReadVarint()\n", readerName)
			switch bt {
			case "int8":
				fmt.Fprintf(sb, "\tif tmp < -128 || tmp > 127 { panic(fmt.Sprintf(\"ffi: int8 overflow: %%d\", tmp)) }\n")
			case "int16":
				fmt.Fprintf(sb, "\tif tmp < -32768 || tmp > 32767 { panic(fmt.Sprintf(\"ffi: int16 overflow: %%d\", tmp)) }\n")
			case "int32":
				fmt.Fprintf(sb, "\tif tmp < -2147483648 || tmp > 2147483647 { panic(fmt.Sprintf(\"ffi: int32 overflow: %%d\", tmp)) }\n")
			case "uint8", "byte":
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > 255 { panic(fmt.Sprintf(\"ffi: uint8 overflow: %%d\", tmp)) }\n")
			case "uint16":
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > 65535 { panic(fmt.Sprintf(\"ffi: uint16 overflow: %%d\", tmp)) }\n")
			case "uint32":
				fmt.Fprintf(sb, "\tif tmp < 0 || tmp > 4294967295 { panic(fmt.Sprintf(\"ffi: uint32 overflow: %%d\", tmp)) }\n")
			case "uint", "uint64":
				fmt.Fprintf(sb, "\tif tmp < 0 { panic(fmt.Sprintf(\"ffi: uint overflow: %%d\", tmp)) }\n")
			case "int":
				// Go's int is at least 32 bits; generated bindings assume a 64-bit VM integer.
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
			fmt.Fprintf(sb, "\t// Restoring %s from Handle\n", pType)
			if isHost {
				fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if obj, ok := registry.Get(id); ok { %s = obj.(%s) } else { return nil, fmt.Errorf(\"invalid handle ID: %%d\", id) } }\n", readerName, varName, g.toGoType(pType))
			} else {
				fmt.Fprintf(sb, "\tif id := uint32(%s.ReadUvarint()); id != 0 { if __p.registry != nil { if obj, ok := __p.registry.Get(id); ok { %s = obj.(%s) } } }\n", readerName, varName, g.toGoType(pType))
			}
		}
	}
}
