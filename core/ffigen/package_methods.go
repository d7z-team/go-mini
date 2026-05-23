package ffigen

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
)

func findMethodsForStruct(files []*ast.File, structName string) []*ast.FuncDecl {
	var res []*ast.FuncDecl
	for _, f := range files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) > 0 {
				recvType := fd.Recv.List[0].Type
				if star, ok := recvType.(*ast.StarExpr); ok {
					recvType = star.X
				}
				if ident, ok := recvType.(*ast.Ident); ok && ident.Name == structName {
					if fd.Name.IsExported() {
						res = append(res, fd)
					}
				}
			}
		}
	}
	return res
}

func (g *Generator) synthesizeInterface(methods []*ast.FuncDecl, addReceiver bool) *ast.InterfaceType {
	iface := &ast.InterfaceType{
		Methods: &ast.FieldList{},
	}
	for _, md := range methods {
		ft := *md.Type
		newParams := make([]*ast.Field, 0, len(ft.Params.List)+1)

		if addReceiver {
			hasContext := false
			if len(ft.Params.List) > 0 {
				pType := g.typeToString(ft.Params.List[0].Type)
				if pType == "context.Context" || pType == "Context" {
					hasContext = true
				}
			}

			// Ensure receiver has a name
			recvField := *md.Recv.List[0]
			recvField.Names = []*ast.Ident{ast.NewIdent("__recv")}

			if hasContext {
				newParams = append(newParams, ft.Params.List[0])
				newParams = append(newParams, &recvField)
				newParams = append(newParams, ft.Params.List[1:]...)
			} else {
				newParams = append(newParams, &recvField)
				newParams = append(newParams, ft.Params.List...)
			}
			ft.Params = &ast.FieldList{List: newParams}
		}

		iface.Methods.List = append(iface.Methods.List, &ast.Field{
			Names: []*ast.Ident{md.Name},
			Type:  &ft,
			Doc:   md.Doc,
		})
	}
	return iface
}

func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.BasicLit:
		return t.Value
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.BinaryExpr:
		return exprToString(t.X) + " " + t.Op.String() + " " + exprToString(t.Y)
	default:
		var buf bytes.Buffer
		if err := format.Node(&buf, token.NewFileSet(), expr); err != nil {
			return ""
		}
		return buf.String()
	}
}
