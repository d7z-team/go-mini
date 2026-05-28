package ffigen

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (g *Generator) emitWrite(sb *strings.Builder, prefix, pType string, expr ast.Expr, structs map[string]*ast.StructType, bufName, moduleName string, interfaceSchemas map[string]string, isHost bool) {
	if pType == "Void" {
		return
	}
	if elemType, ok := readChanElemType(pType); ok {
		g.emitChannelWrite(sb, prefix, pType, elemType, structs, bufName, moduleName, interfaceSchemas, isHost)
		return
	}
	if isBytesRefTypeString(pType) {
		fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteBytes(nil)\n\t} else {\n\t\t%s.WriteBytes(%s.Value)\n\t}\n", prefix, bufName, bufName, prefix)
		return
	}
	if interfaceName, schemaVar, ok := g.interfaceSchemaForType(pType, moduleName, interfaceSchemas); ok {
		g.emitInterfaceWrite(sb, prefix, interfaceName, schemaVar, bufName, isHost)
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
			g.emitWrite(sb, "item", itemType, nil, structs, bufName, moduleName, interfaceSchemas, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(pType); ok {
			fmt.Fprintf(sb, "\t%s.WriteUvarint(uint64(len(%s)))\n\tfor k, v := range %s {\n", bufName, prefix, prefix)
			g.emitWrite(sb, "k", kType, nil, structs, bufName, moduleName, interfaceSchemas, isHost)
			g.emitWrite(sb, "v", vType, nil, structs, bufName, moduleName, interfaceSchemas, isHost)
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
				g.emitWrite(sb, prefix+"."+fn, fMap[fn], nil, structs, bufName, moduleName, interfaceSchemas, isHost)
			}
		} else {
			unsupportedBareFFIType(pType)
		}
	}
}

func (g *Generator) emitReadAssign(sb *strings.Builder, varName, pType string, expr ast.Expr, structs map[string]*ast.StructType, readerName, moduleName string, interfaceSchemas map[string]string, isHost bool) {
	if pType == "Void" {
		fmt.Fprintf(sb, "\t%s = ffigo.Void{}\n", varName)
		return
	}
	if elemType, ok := readChanElemType(pType); ok {
		g.emitChannelReadAssign(sb, varName, pType, elemType, structs, readerName, moduleName, interfaceSchemas, isHost)
		return
	}
	if isBytesRefTypeString(pType) {
		fmt.Fprintf(sb, "\t{\n\tbytes, _ := %s.ReadBytes()\n\t%s = &ffigo.BytesRef{Value: bytes}\n\t}\n", readerName, varName)
		return
	}
	if interfaceName, _, ok := g.interfaceSchemaForType(pType, moduleName, interfaceSchemas); ok {
		g.emitInterfaceReadAssign(sb, varName, interfaceName, g.toGoType(pType), readerName, isHost)
		return
	}
	if isRefTypeString(pType) {
		typeID := g.refTypeID(pType, moduleName)
		fmt.Fprintf(sb, "\t// HostRef<T> is restored from the opaque handle ID written on the FFI wire.\n")
		if isHost {
			fmt.Fprintf(sb, "\tif rawID, _ := %s.ReadUvarint(); rawID != 0 { id := uint32(rawID); if obj, err := registry.GetTypedWithAudit(id, \"%s\"); err == nil { %s = obj.(%s) } else { return nil, fmt.Errorf(\"FFI restore param '%%s' failed: %%v\", \"%s\", err) } }\n", readerName, typeID, varName, g.toGoType(pType), varName)
		} else {
			fmt.Fprintf(sb, "\tif rawID, _ := %s.ReadUvarint(); rawID != 0 { id := uint32(rawID); if __p.registry != nil { if obj, ok := __p.registry.GetTyped(id, \"%s\"); ok { %s = obj.(%s) } } }\n", readerName, typeID, varName, g.toGoType(pType))
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
			fmt.Fprintf(sb, "\t{\n\ttmp, _ := %s.ReadVarint()\n", readerName)
			switch bt {
			case "int8":
				g.emitReadOverflowCheck(sb, "tmp < -128 || tmp > 127", "int8", isHost)
			case "int16":
				g.emitReadOverflowCheck(sb, "tmp < -32768 || tmp > 32767", "int16", isHost)
			case "int32":
				g.emitReadOverflowCheck(sb, "tmp < -2147483648 || tmp > 2147483647", "int32", isHost)
			case "int":
				// Go's int is at least 32 bits; generated bindings assume a 64-bit VM integer.
			}
			fmt.Fprintf(sb, "\t%s = %s(tmp)\n\t}\n", varName, gt)
			return
		case codecUnsignedBasic(bt):
			fmt.Fprintf(sb, "\t{\n\ttmp, _ := %s.ReadVarint()\n", readerName)
			if maxLiteral := codecUnsignedMaxLiteral(bt); maxLiteral != "" {
				g.emitReadOverflowCheck(sb, "tmp < 0 || tmp > "+maxLiteral, bt, isHost)
			} else if isHost {
				g.emitReadOverflowCheck(sb, "tmp < 0", bt, isHost)
			}
			fmt.Fprintf(sb, "\t%s = %s(tmp)\n\t}\n", varName, gt)
			return
		case strings.HasPrefix(bt, "float"):
			fmt.Fprintf(sb, "\t{\n\ttmp, _ := %s.ReadFloat64()\n\t%s = %s(tmp)\n\t}\n", readerName, varName, gt)
			return
		case bt == "string":
			fmt.Fprintf(sb, "\t{\n\ttmp, _ := %s.ReadString()\n\t%s = %s(tmp)\n\t}\n", readerName, varName, gt)
			return
		case bt == "bool":
			fmt.Fprintf(sb, "\t{\n\ttmp, _ := %s.ReadBool()\n\t%s = %s(tmp)\n\t}\n", readerName, varName, gt)
			return
		}
	}

	switch pType {
	case "[]byte", "TypeBytes", "Array<Uint8>", "Array<byte>":
		fmt.Fprintf(sb, "\t%s, _ = %s.ReadBytes()\n", varName, readerName)
	case "bool", "Bool":
		fmt.Fprintf(sb, "\t%s, _ = %s.ReadBool()\n", varName, readerName)
	case "float64", "Float64", "float32", "Float32":
		fmt.Fprintf(sb, "\t%s, _ = %s.ReadFloat64()\n", varName, readerName)
	case "Any", "any":
		if isHost {
			fmt.Fprintf(sb, "\trawVal, _ = %s.ReadAny()\n\tswitch rv := rawVal.(type) {\n\tcase ffigo.InterfaceData:\n\t\tif rv.Handle != 0 { return nil, fmt.Errorf(\"FFI Any param '%%s' cannot carry host interface handle\", \"%s\") }\n\t\t%s = rv\n\tcase ffigo.ErrorData:\n\t\tif rv.Handle != 0 { return nil, fmt.Errorf(\"FFI Any param '%%s' cannot carry host error handle\", \"%s\") }\n\t\t%s = rv\n\tdefault:\n\t\t%s = rawVal\n\t}\n", readerName, varName, varName, varName, varName, varName)
		} else {
			fmt.Fprintf(sb, "\t%s, _ = %s.ReadAny()\n", varName, readerName)
		}
	default:
		if itemType, ok := readArrayItemType(pType); ok {
			suffix := generatedIdentSuffix(varName)
			lenVar := "l_" + suffix
			idxVar := "i_" + suffix
			fmt.Fprintf(sb, "\t%s, _ := %s.ReadCount(ffigo.MaxWireCollectionItems, \"array\")\n\t%s = make(%s, %s)\n\tfor %s := 0; %s < %s; %s++ {\n", lenVar, readerName, varName, g.toGoType(pType), lenVar, idxVar, idxVar, lenVar, idxVar)
			g.emitReadAssign(sb, fmt.Sprintf("%s[%s]", varName, idxVar), itemType, nil, structs, readerName, moduleName, interfaceSchemas, isHost)
			fmt.Fprintf(sb, "\t}\n")
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(pType); ok {
			suffix := generatedIdentSuffix(varName)
			lenVar := "l_" + suffix
			idxVar := "i_" + suffix
			fmt.Fprintf(sb, "\t%s, _ := %s.ReadCount(ffigo.MaxWireCollectionItems, \"map\")\n\t%s = make(%s)\n\tfor %s := 0; %s < %s; %s++ {\n\t\tvar k %s\n\t\tvar v %s\n", lenVar, readerName, varName, g.toGoType(pType), idxVar, idxVar, lenVar, idxVar, g.toGoType(kType), g.toGoType(vType))
			g.emitReadAssign(sb, "k", kType, nil, structs, readerName, moduleName, interfaceSchemas, isHost)
			g.emitReadAssign(sb, "v", vType, nil, structs, readerName, moduleName, interfaceSchemas, isHost)
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
				g.emitReadAssign(sb, varName+"."+fn, fMap[fn], nil, structs, readerName, moduleName, interfaceSchemas, isHost)
			}
		} else {
			unsupportedBareFFIType(pType)
		}
	}
}

func (g *Generator) emitReadOverflowCheck(sb *strings.Builder, condition, typeName string, isHost bool) {
	if isHost {
		fmt.Fprintf(sb, "\tif %s { return nil, fmt.Errorf(\"ffi: %s overflow: %%d\", tmp) }\n", condition, typeName)
		return
	}
	fmt.Fprintf(sb, "\tif %s { panic(fmt.Sprintf(\"ffi: %s overflow: %%d\", tmp)) }\n", condition, typeName)
}

func (g *Generator) emitChannelWrite(sb *strings.Builder, prefix, pType, elemType string, structs map[string]*ast.StructType, bufName, moduleName string, interfaceSchemas map[string]string, isHost bool) {
	elemType = g.newDisplayTypeResolver(moduleName).NormalizeTypeString(elemType)
	suffix := generatedIdentSuffix(prefix + "_" + bufName)
	registryExpr := "__p.channelRegistry()"
	if isHost {
		registryExpr = "ffigo.ChannelRegistryFromContext(ctx)"
	}
	dir := channelDirectionLiteral(pType)
	canRecv := channelTypeCanRecv(pType)
	canSend := channelTypeCanSend(pType)
	elemGoType := g.toGoType(elemType)
	fmt.Fprintf(sb, "\t{\n")
	fmt.Fprintf(sb, "\t\tchannelID_%s := uint64(0)\n", suffix)
	fmt.Fprintf(sb, "\t\tif %s != nil {\n", prefix)
	fmt.Fprintf(sb, "\t\t\tif channels_%s := %s; channels_%s != nil {\n", suffix, registryExpr, suffix)
	fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s := ffigo.ChannelEndpointFuncs{Elem: %q, Dir: %s}\n", suffix, elemType, dir)
	if canRecv {
		valueVar := "channelValue_" + suffix
		payloadVar := "channelPayload_" + suffix
		fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s.OnRecv = func(channelCtx context.Context) ([]byte, bool, error) {\n", suffix)
		fmt.Fprintf(sb, "\t\t\t\t\tselect {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase %s, ok := <-%s:\n", valueVar, prefix)
		fmt.Fprintf(sb, "\t\t\t\t\t\tif !ok { channels_%s.UnregisterChannel(channelID_%s); return nil, false, nil }\n", suffix, suffix)
		fmt.Fprintf(sb, "\t\t\t\t\t\t%s := ffigo.GetBuffer()\n", payloadVar)
		fmt.Fprintf(sb, "\t\t\t\t\t\tdefer ffigo.ReleaseBuffer(%s)\n", payloadVar)
		g.emitWrite(sb, valueVar, elemType, nil, structs, payloadVar, moduleName, interfaceSchemas, isHost)
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn append([]byte(nil), %s.Bytes()...), true, nil\n", payloadVar)
		fmt.Fprintf(sb, "\t\t\t\t\tcase <-channelCtx.Done():\n")
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn nil, false, channelCtx.Err()\n")
		fmt.Fprintf(sb, "\t\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s.OnTryRecv = func() ([]byte, bool, bool, error) {\n", suffix)
		fmt.Fprintf(sb, "\t\t\t\t\tselect {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase %s, ok := <-%s:\n", valueVar, prefix)
		fmt.Fprintf(sb, "\t\t\t\t\t\tif !ok { channels_%s.UnregisterChannel(channelID_%s); return nil, false, true, nil }\n", suffix, suffix)
		fmt.Fprintf(sb, "\t\t\t\t\t\t%s := ffigo.GetBuffer()\n", payloadVar)
		fmt.Fprintf(sb, "\t\t\t\t\t\tdefer ffigo.ReleaseBuffer(%s)\n", payloadVar)
		g.emitWrite(sb, valueVar, elemType, nil, structs, payloadVar, moduleName, interfaceSchemas, isHost)
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn append([]byte(nil), %s.Bytes()...), true, true, nil\n", payloadVar)
		fmt.Fprintf(sb, "\t\t\t\t\tdefault:\n")
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn nil, false, false, nil\n")
		fmt.Fprintf(sb, "\t\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\t}\n")
	}
	if canSend {
		valueVar := "channelValue_" + suffix
		readerVar := "channelReader_" + suffix
		payloadVar := "channelPayload_" + suffix
		fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s.OnSend = func(channelCtx context.Context, data []byte) error {\n", suffix)
		fmt.Fprintf(sb, "\t\t\t\t\tvar %s %s\n", valueVar, elemGoType)
		fmt.Fprintf(sb, "\t\t\t\t\t%s := ffigo.NewReader(data)\n", readerVar)
		g.emitReadAssign(sb, valueVar, elemType, nil, structs, readerVar, moduleName, interfaceSchemas, isHost)
		fmt.Fprintf(sb, "\t\t\t\t\tif err := %s.Err(); err != nil { return err }\n", readerVar)
		fmt.Fprintf(sb, "\t\t\t\t\tselect {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase %s <- %s:\n", prefix, valueVar)
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn nil\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase <-channelCtx.Done():\n")
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn channelCtx.Err()\n")
		fmt.Fprintf(sb, "\t\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s.OnTrySend = func(data []byte) (bool, error) {\n", suffix)
		fmt.Fprintf(sb, "\t\t\t\t\tvar %s %s\n", valueVar, elemGoType)
		fmt.Fprintf(sb, "\t\t\t\t\t%s := ffigo.NewReader(data)\n", readerVar)
		g.emitReadAssign(sb, valueVar, elemType, nil, structs, readerVar, moduleName, interfaceSchemas, isHost)
		fmt.Fprintf(sb, "\t\t\t\t\tif err := %s.Err(); err != nil { return false, err }\n", readerVar)
		fmt.Fprintf(sb, "\t\t\t\t\tselect {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase %s <- %s:\n", prefix, valueVar)
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn true, nil\n")
		fmt.Fprintf(sb, "\t\t\t\t\tdefault:\n")
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn false, nil\n")
		fmt.Fprintf(sb, "\t\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\t}\n")
		_ = payloadVar
	}
	if canSend {
		fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s.OnClose = func() error { defer channels_%s.UnregisterChannel(channelID_%s); close(%s); return nil }\n", suffix, suffix, suffix, prefix)
	} else {
		fmt.Fprintf(sb, "\t\t\t\tchannelEndpoint_%s.OnClose = func() error { channels_%s.UnregisterChannel(channelID_%s); return nil }\n", suffix, suffix, suffix)
	}
	fmt.Fprintf(sb, "\t\t\t\tchannelID_%s = channels_%s.RegisterChannel(channelEndpoint_%s)\n", suffix, suffix, suffix)
	fmt.Fprintf(sb, "\t\t\t}\n")
	fmt.Fprintf(sb, "\t\t}\n")
	fmt.Fprintf(sb, "\t\t%s.WriteUvarint(channelID_%s)\n", bufName, suffix)
	fmt.Fprintf(sb, "\t}\n")
}

func (g *Generator) emitChannelReadAssign(sb *strings.Builder, varName, pType, elemType string, structs map[string]*ast.StructType, readerName, moduleName string, interfaceSchemas map[string]string, isHost bool) {
	elemType = g.newDisplayTypeResolver(moduleName).NormalizeTypeString(elemType)
	suffix := generatedIdentSuffix(varName + "_" + readerName)
	registryExpr := "__p.channelRegistry()"
	ctxExpr := "context.Background()"
	if isHost {
		registryExpr = "ffigo.ChannelRegistryFromContext(ctx)"
		ctxExpr = "ctx"
	}
	elemGoType := g.toGoType(elemType)
	canRecv := channelTypeCanRecv(pType)
	canSend := channelTypeCanSend(pType)
	fmt.Fprintf(sb, "\t{\n")
	fmt.Fprintf(sb, "\t\tchannelID_%s, _ := %s.ReadUvarint()\n", suffix, readerName)
	fmt.Fprintf(sb, "\t\tif channelID_%s != 0 {\n", suffix)
	fmt.Fprintf(sb, "\t\t\tchannels_%s := %s\n", suffix, registryExpr)
	if isHost {
		fmt.Fprintf(sb, "\t\t\tif channels_%s == nil { return nil, fmt.Errorf(\"FFI channel param '%%s' failed: missing channel registry\", %q) }\n", suffix, varName)
		fmt.Fprintf(sb, "\t\t\tendpoint_%s, ok := channels_%s.LookupChannel(channelID_%s)\n", suffix, suffix, suffix)
		fmt.Fprintf(sb, "\t\t\tif !ok { return nil, fmt.Errorf(\"FFI channel param '%%s' failed: unknown channel endpoint %%d\", %q, channelID_%s) }\n", varName, suffix)
	} else {
		fmt.Fprintf(sb, "\t\t\tif channels_%s == nil { panic(fmt.Sprintf(\"FFI channel result '%s' failed: missing channel registry\")) }\n", suffix, varName)
		fmt.Fprintf(sb, "\t\t\tendpoint_%s, ok := channels_%s.LookupChannel(channelID_%s)\n", suffix, suffix, suffix)
		fmt.Fprintf(sb, "\t\t\tif !ok { panic(fmt.Sprintf(\"FFI channel result '%s' failed: unknown channel endpoint %%d\", channelID_%s)) }\n", varName, suffix)
	}
	fmt.Fprintf(sb, "\t\t\tbridgeChan_%s := make(chan %s)\n", suffix, elemGoType)
	fmt.Fprintf(sb, "\t\t\t%s = bridgeChan_%s\n", varName, suffix)
	if canRecv {
		valueVar := "channelValue_" + suffix
		payloadVar := "channelPayload_" + suffix
		readerVar := "channelReader_" + suffix
		closeLine := "\t\t\t\tdefer close(bridgeChan_" + suffix + ")\n"
		if canSend {
			closeLine = ""
		}
		fmt.Fprintf(sb, "\t\t\tgo func() {\n")
		sb.WriteString(closeLine)
		fmt.Fprintf(sb, "\t\t\t\tfor {\n")
		fmt.Fprintf(sb, "\t\t\t\t\t%s, ok, err := endpoint_%s.Recv(%s)\n", payloadVar, suffix, ctxExpr)
		fmt.Fprintf(sb, "\t\t\t\t\tif err != nil || !ok { return }\n")
		fmt.Fprintf(sb, "\t\t\t\t\tvar %s %s\n", valueVar, elemGoType)
		fmt.Fprintf(sb, "\t\t\t\t\t%s := ffigo.NewReader(%s)\n", readerVar, payloadVar)
		g.emitReadAssign(sb, valueVar, elemType, nil, structs, readerVar, moduleName, interfaceSchemas, isHost)
		fmt.Fprintf(sb, "\t\t\t\t\tif %s.Err() != nil { return }\n", readerVar)
		fmt.Fprintf(sb, "\t\t\t\t\tselect {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase bridgeChan_%s <- %s:\n", suffix, valueVar)
		fmt.Fprintf(sb, "\t\t\t\t\tcase <-%s.Done():\n", ctxExpr)
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn\n")
		fmt.Fprintf(sb, "\t\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t}()\n")
	}
	if canSend {
		valueVar := "channelValue_" + suffix
		payloadVar := "channelPayload_" + suffix
		fmt.Fprintf(sb, "\t\t\tgo func() {\n")
		fmt.Fprintf(sb, "\t\t\t\tfor {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tselect {\n")
		fmt.Fprintf(sb, "\t\t\t\t\tcase %s, ok := <-bridgeChan_%s:\n", valueVar, suffix)
		fmt.Fprintf(sb, "\t\t\t\t\t\tif !ok { _ = endpoint_%s.Close(); return }\n", suffix)
		fmt.Fprintf(sb, "\t\t\t\t\t\t%s := ffigo.GetBuffer()\n", payloadVar)
		g.emitWrite(sb, valueVar, elemType, nil, structs, payloadVar, moduleName, interfaceSchemas, isHost)
		fmt.Fprintf(sb, "\t\t\t\t\t\tchannelBytes_%s := append([]byte(nil), %s.Bytes()...)\n", suffix, payloadVar)
		fmt.Fprintf(sb, "\t\t\t\t\t\tffigo.ReleaseBuffer(%s)\n", payloadVar)
		fmt.Fprintf(sb, "\t\t\t\t\t\tif err := endpoint_%s.Send(%s, channelBytes_%s); err != nil { return }\n", suffix, ctxExpr, suffix)
		fmt.Fprintf(sb, "\t\t\t\t\tcase <-%s.Done():\n", ctxExpr)
		fmt.Fprintf(sb, "\t\t\t\t\t\treturn\n")
		fmt.Fprintf(sb, "\t\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t\t}()\n")
	}
	fmt.Fprintf(sb, "\t\t}\n")
	fmt.Fprintf(sb, "\t}\n")
}

func buildInterfaceSchemaVars(interfaceFFI map[string]bool, displayTypeName func(string) string) map[string]string {
	if len(interfaceFFI) == 0 {
		return nil
	}
	out := make(map[string]string, len(interfaceFFI))
	for name := range interfaceFFI {
		displayName := displayTypeName(name)
		out[displayName] = interfaceSchemaVarName(displayName)
	}
	return out
}

func (g *Generator) interfaceSchemaForType(typeName, moduleName string, interfaceSchemas map[string]string) (string, string, bool) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || len(interfaceSchemas) == 0 {
		return "", "", false
	}
	if isBytesRefTypeString(typeName) {
		return "", "", false
	}
	if _, ok := ffigo.RefElementType(typeName); ok {
		return "", "", false
	}
	if schemaVar, ok := interfaceSchemas[typeName]; ok {
		return typeName, schemaVar, true
	}
	displayName := g.newDisplayTypeResolver(moduleName).NormalizeTypeString(typeName)
	if schemaVar, ok := interfaceSchemas[displayName]; ok {
		return displayName, schemaVar, true
	}
	return "", "", false
}

func (g *Generator) collectReferencedInterfaceNames(methods []generatedMethod, moduleName string, interfaceSchemas map[string]string) []string {
	if len(interfaceSchemas) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(typeName string) {
		if interfaceName, _, ok := g.interfaceSchemaForType(typeName, moduleName, interfaceSchemas); ok {
			seen[interfaceName] = true
			return
		}
		if itemType, ok := readArrayItemType(typeName); ok {
			visit(itemType)
			return
		}
		if kType, vType, ok := readMapKeyValueTypes(typeName); ok {
			visit(kType)
			visit(vType)
			return
		}
		if elemType, ok := readChanElemType(typeName); ok {
			visit(elemType)
		}
	}
	for _, method := range methods {
		for _, param := range method.Params {
			if param.Context {
				continue
			}
			visit(param.RawType)
			visit(param.VMType)
		}
		for _, result := range method.Results {
			if result.Error {
				continue
			}
			visit(result.RawType)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (g *Generator) emitInterfaceWrite(sb *strings.Builder, prefix, interfaceName, schemaVar, bufName string, isHost bool) {
	fmt.Fprintf(sb, "\t// Named FFI interfaces cross the wire as a host handle plus method schema.\n")
	fmt.Fprintf(sb, "\tif %s == nil {\n\t\t%s.WriteRawInterface(0, nil)\n\t} else {\n", prefix, bufName)
	if isHost {
		fmt.Fprintf(sb, "\t\thandle := uint32(0)\n\t\tif registry != nil { handle = registry.RegisterTyped(%s, %q) }\n", prefix, interfaceName)
	} else {
		fmt.Fprintf(sb, "\t\thandle := uint32(0)\n\t\tif __p.registry != nil { handle = __p.registry.RegisterTyped(%s, %q) }\n", prefix, interfaceName)
	}
	fmt.Fprintf(sb, "\t\t%s.WriteRawInterface(handle, %s.MethodStringMap())\n\t}\n", bufName, schemaVar)
}

func (g *Generator) emitInterfaceReadAssign(sb *strings.Builder, varName, interfaceName, goType, readerName string, isHost bool) {
	dataVar := "ifaceData_" + generatedIdentSuffix(varName)
	fmt.Fprintf(sb, "\t%s, _ := %s.ReadRawInterface()\n", dataVar, readerName)
	fmt.Fprintf(sb, "\tif %s.Handle != 0 {\n", dataVar)
	if isHost {
		fmt.Fprintf(sb, "\t\tif registry == nil { return nil, fmt.Errorf(\"FFI restore interface param '%%s' failed: missing registry\", %q) }\n", varName)
		fmt.Fprintf(sb, "\t\tobj, err := registry.GetWithAudit(%s.Handle)\n", dataVar)
		fmt.Fprintf(sb, "\t\tif err != nil { return nil, fmt.Errorf(\"FFI restore interface param '%%s' failed: %%v\", %q, err) }\n", varName)
		fmt.Fprintf(sb, "\t\tcast, ok := obj.(%s)\n", goType)
		fmt.Fprintf(sb, "\t\tif !ok { return nil, fmt.Errorf(\"FFI restore interface param '%%s' failed: expected %s, got %%T\", %q, obj) }\n", interfaceName, varName)
		fmt.Fprintf(sb, "\t\t%s = cast\n", varName)
	} else {
		fmt.Fprintf(sb, "\t\tif __p.registry != nil {\n")
		fmt.Fprintf(sb, "\t\t\tif obj, ok := __p.registry.Get(%s.Handle); ok {\n", dataVar)
		fmt.Fprintf(sb, "\t\t\t\tcast, ok := obj.(%s)\n", goType)
		fmt.Fprintf(sb, "\t\t\t\tif !ok { panic(fmt.Sprintf(\"FFI restore interface result '%s' failed: expected %s, got %%T\", obj)) }\n", varName, interfaceName)
		fmt.Fprintf(sb, "\t\t\t\t%s = cast\n", varName)
		fmt.Fprintf(sb, "\t\t\t}\n\t\t}\n")
	}
	fmt.Fprintf(sb, "\t} else if len(%s.Methods) != 0 {\n", dataVar)
	if isHost {
		fmt.Fprintf(sb, "\t\treturn nil, fmt.Errorf(\"FFI restore interface param '%%s' failed: %s has method schema but no host handle\", %q)\n", interfaceName, varName)
	} else {
		fmt.Fprintf(sb, "\t\tpanic(fmt.Sprintf(\"FFI restore interface result '%s' failed: %s has method schema but no host handle\"))\n", varName, interfaceName)
	}
	fmt.Fprintf(sb, "\t}\n")
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
