package ffigo

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func (c *GoToASTConverter) canonicalBuiltinTypeName(name string) string {
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

func (c *GoToASTConverter) typeToString(e ast.Expr) string {
	return c.typeToStringWithDepth(e, 0)
}

func (c *GoToASTConverter) typeToStringWithDepth(e ast.Expr, depth int) string {
	if e == nil || depth > 10 {
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
	case *ast.StarExpr:
		return string(miniast.GoMiniType(c.typeToStringWithDepth(t.X, depth+1)).ToPtr())
	case *ast.MapType:
		return string(miniast.CreateMapType(
			miniast.GoMiniType(c.typeToStringWithDepth(t.Key, depth+1)),
			miniast.GoMiniType(c.typeToStringWithDepth(t.Value, depth+1)),
		))
	case *ast.SelectorExpr:
		return c.typeToStringWithDepth(t.X, depth+1) + "." + t.Sel.Name
	case *ast.Ellipsis:
		return string(miniast.CreateArrayType(miniast.GoMiniType(c.typeToStringWithDepth(t.Elt, depth+1))))
	case *ast.InterfaceType:
		return c.expandInterface(t, depth+1)
	}
	return string(miniast.TypeAny)
}

func (c *GoToASTConverter) expandInterface(t *ast.InterfaceType, depth int) string {
	var methods []string
	if t.Methods != nil {
		for _, m := range t.Methods.List {
			if len(m.Names) > 0 {
				// 提取方法签名：Read(String) String
				var params []string
				var returns string
				if fn, ok := m.Type.(*ast.FuncType); ok {
					if fn.Params != nil {
						for _, p := range fn.Params.List {
							pType := c.typeToStringWithDepth(p.Type, depth+1)
							count := len(p.Names)
							if count == 0 {
								count = 1
							}
							for i := 0; i < count; i++ {
								params = append(params, pType)
							}
						}
					}
					if fn.Results != nil && len(fn.Results.List) > 0 {
						var resTypes []string
						for _, r := range fn.Results.List {
							resTypes = append(resTypes, c.typeToStringWithDepth(r.Type, depth+1))
						}
						if len(resTypes) > 1 {
							returns = " tuple(" + strings.Join(resTypes, ", ") + ")"
						} else {
							returns = " " + resTypes[0]
						}
					} else {
						returns = " Void"
					}
				}
				sig := fmt.Sprintf("(%s)%s", strings.Join(params, ","), returns)
				for _, name := range m.Names {
					methods = append(methods, name.Name+sig)
				}
			} else {
				// 嵌入接口：递归展开
				embedded := c.typeToStringWithDepth(m.Type, depth+1)
				if strings.HasPrefix(embedded, "interface{") {
					inner := strings.TrimSuffix(strings.TrimPrefix(embedded, "interface{"), "}")
					if inner != "" {
						methods = append(methods, strings.Split(inner, ";")...)
					}
				}
			}
		}
	}
	if len(methods) == 0 {
		return string(miniast.TypeAny)
	}
	return fmt.Sprintf("interface{%s}", strings.Join(methods, ";"))
}
