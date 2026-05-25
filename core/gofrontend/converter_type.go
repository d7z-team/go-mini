package gofrontend

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func (c *Converter) canonicalBuiltinTypeName(name string) string {
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune":
		return string(miniast.TypeInt64)
	case "float32", "float64":
		return string(miniast.TypeFloat64)
	case "string":
		return string(miniast.TypeString)
	case "error":
		return string(miniast.TypeError)
	case "bool":
		return string(miniast.TypeBool)
	case "any", "interface{}":
		return string(miniast.TypeAny)
	}
	return name
}

func (c *Converter) typeToString(e ast.Expr) string {
	return c.typeToStringWithDepth(e, 0)
}

func (c *Converter) typeToStringWithDepth(e ast.Expr, depth int) string {
	if e == nil {
		return string(miniast.TypeAny)
	}
	if depth > 10 {
		return string(miniast.TypeAny)
	}
	switch t := e.(type) {
	case *ast.BasicLit:
		val := t.Value
		if t.Kind == token.STRING && len(val) >= 2 {
			if unquoted, err := strconv.Unquote(val); err == nil {
				val = unquoted
			} else {
				val = val[1 : len(val)-1]
			}
		}
		return val
	case *ast.Ident:
		name := t.Name
		if name == "byte" {
			return string(miniast.TypeInt64)
		}
		name = c.canonicalBuiltinTypeName(name)
		// 检查是否是当前程序中定义的接口名
		if iface, ok := c.interfaces[name]; ok {
			return c.expandInterface(iface, depth+1)
		}
		return name
	case *ast.ArrayType:
		if ident, ok := t.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
			return string(miniast.TypeBytes)
		}
		return string(miniast.CreateArrayType(miniast.GoMiniType(c.typeToStringWithDepth(t.Elt, depth+1))))
	case *ast.ChanType:
		elem := miniast.GoMiniType(c.typeToStringWithDepth(t.Value, depth+1))
		switch t.Dir {
		case ast.RECV:
			return string(miniast.CreateRecvChanType(elem))
		case ast.SEND:
			return string(miniast.CreateSendChanType(elem))
		default:
			return string(miniast.CreateChanType(elem))
		}
	case *ast.StarExpr:
		return string(miniast.GoMiniType(c.typeToStringWithDepth(t.X, depth+1)).ToPtr())
	case *ast.MapType:
		return string(miniast.CreateMapType(
			miniast.GoMiniType(c.typeToStringWithDepth(t.Key, depth+1)),
			miniast.GoMiniType(c.typeToStringWithDepth(t.Value, depth+1)),
		))
	case *ast.SelectorExpr:
		return string(miniast.CreateQualifiedType(c.typeToStringWithDepth(t.X, depth+1), t.Sel.Name))
	case *ast.Ellipsis:
		return string(miniast.CreateArrayType(miniast.GoMiniType(c.typeToStringWithDepth(t.Elt, depth+1))))
	case *ast.InterfaceType:
		return c.expandInterface(t, depth+1)
	case *ast.StructType:
		if t.Fields == nil || len(t.Fields.List) == 0 {
			return string(miniast.TypeVoid)
		}
		c.addError(e, "Go struct type literals are only supported for empty struct{} channel signals")
		return string(miniast.TypeAny)
	case *ast.FuncType:
		var params []miniast.FunctionParam
		if t.Params != nil {
			for _, p := range t.Params.List {
				pType := miniast.GoMiniType(c.typeToStringWithDepth(p.Type, depth+1))
				if _, ok := p.Type.(*ast.Ellipsis); ok {
					if elem, ok := pType.ReadArrayItemType(); ok {
						pType = elem
					}
				}
				count := len(p.Names)
				if count == 0 {
					count = 1
				}
				for i := 0; i < count; i++ {
					params = append(params, miniast.FunctionParam{Type: pType})
				}
			}
		}
		retType := miniast.TypeVoid
		if t.Results != nil {
			var results []miniast.GoMiniType
			for _, r := range t.Results.List {
				rType := miniast.GoMiniType(c.typeToStringWithDepth(r.Type, depth+1))
				count := len(r.Names)
				if count == 0 {
					count = 1
				}
				for i := 0; i < count; i++ {
					results = append(results, rType)
				}
			}
			if len(results) > 0 {
				retType = miniast.CreateTupleType(results...)
			}
		}
		return string(miniast.CreateFunctionType(params, retType, lastParamIsVariadic(t.Params)))
	}
	c.addError(e, fmt.Sprintf("不支持的类型语法: %T", e))
	return string(miniast.TypeAny)
}

func (c *Converter) expandInterface(t *ast.InterfaceType, depth int) string {
	methods := make(map[string]*miniast.FunctionType)
	if t.Methods != nil {
		for _, m := range t.Methods.List {
			if len(m.Names) > 0 {
				var params []miniast.FunctionParam
				returns := miniast.TypeVoid
				var variadic bool
				if fn, ok := m.Type.(*ast.FuncType); ok {
					if fn.Params != nil {
						for _, p := range fn.Params.List {
							pType := miniast.GoMiniType(c.typeToStringWithDepth(p.Type, depth+1))
							if _, ok := p.Type.(*ast.Ellipsis); ok {
								variadic = true
								if elem, ok := pType.ReadArrayItemType(); ok {
									pType = elem
								}
							}
							count := len(p.Names)
							if count == 0 {
								count = 1
							}
							for i := 0; i < count; i++ {
								params = append(params, miniast.FunctionParam{Type: pType})
							}
						}
					}
					if fn.Results != nil && len(fn.Results.List) > 0 {
						var resTypes []miniast.GoMiniType
						for _, r := range fn.Results.List {
							resTypes = append(resTypes, miniast.GoMiniType(c.typeToStringWithDepth(r.Type, depth+1)))
						}
						returns = miniast.CreateTupleType(resTypes...)
					}
				}
				for _, name := range m.Names {
					methods[name.Name] = &miniast.FunctionType{Params: params, Return: returns, Variadic: variadic}
				}
			} else {
				// 嵌入接口：递归展开
				embedded := c.typeToStringWithDepth(m.Type, depth+1)
				if embeddedMethods, ok := miniast.GoMiniType(embedded).ReadInterfaceMethods(); ok {
					for name, sig := range embeddedMethods {
						methods[name] = sig
					}
				}
			}
		}
	}
	if len(methods) == 0 {
		return string(miniast.TypeAny)
	}
	return string(miniast.CreateInterfaceType(methods))
}

func lastParamIsVariadic(fields *ast.FieldList) bool {
	if fields == nil || len(fields.List) == 0 {
		return false
	}
	if _, ok := fields.List[len(fields.List)-1].Type.(*ast.Ellipsis); ok {
		return true
	}
	return false
}
