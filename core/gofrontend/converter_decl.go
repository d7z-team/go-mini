package gofrontend

import (
	"go/ast"
	"strconv"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func (c *Converter) convertAssignRHS(st *ast.AssignStmt) []miniast.Expr {
	if len(st.Rhs) == 1 {
		rhsExpr := c.convertExpr(st.Rhs[0])
		if len(st.Lhs) == 2 {
			if ta, ok := rhsExpr.(*miniast.TypeAssertExpr); ok {
				ta.Multi = true
			} else if ie, ok := rhsExpr.(*miniast.IndexExpr); ok {
				ie.Multi = true
			} else if recv, ok := rhsExpr.(*miniast.ReceiveExpr); ok {
				recv.Multi = true
			}
		}
		return []miniast.Expr{rhsExpr}
	}

	values := make([]miniast.Expr, 0, len(st.Rhs))
	for _, r := range st.Rhs {
		values = append(values, c.convertExpr(r))
	}
	return values
}

func (c *Converter) convertValueSpecDecl(s *ast.ValueSpec) *miniast.GenDeclStmt {
	kind := miniast.GoMiniType("")
	if s.Type != nil {
		kind = miniast.GoMiniType(c.typeToString(s.Type))
	}
	bindings := make([]miniast.VarBinding, 0, len(s.Names))
	for _, name := range s.Names {
		bindings = append(bindings, miniast.VarBinding{
			Name:     miniast.Ident(name.Name),
			Kind:     kind,
			Inferred: s.Type == nil,
		})
	}
	values := make([]miniast.Expr, 0, len(s.Values))
	for _, value := range s.Values {
		expr := c.convertExpr(value)
		if len(s.Names) == 2 && len(s.Values) == 1 {
			if recv, ok := expr.(*miniast.ReceiveExpr); ok {
				recv.Multi = true
			}
		}
		values = append(values, expr)
	}
	return &miniast.GenDeclStmt{
		BaseNode: miniast.BaseNode{ID: c.genID(s, "decl"), Meta: "decl", Loc: c.extractLoc(s)},
		Bindings: bindings,
		Values:   values,
	}
}

func declValueForBinding(decl *miniast.GenDeclStmt, index int) miniast.Expr {
	if decl == nil || len(decl.Values) == 0 {
		return nil
	}
	if len(decl.Values) == len(decl.Bindings) && index >= 0 && index < len(decl.Values) {
		return decl.Values[index]
	}
	if len(decl.Values) == 1 {
		return decl.Values[0]
	}
	return nil
}

func (c *Converter) convertStruct(name string, s *ast.StructType, loc *miniast.Position, doc string) *miniast.StructStmt {
	res := &miniast.StructStmt{
		BaseNode:  miniast.BaseNode{ID: c.genID(s, "struct"), Meta: "struct", Loc: loc},
		Name:      miniast.Ident(name),
		Fields:    make(map[miniast.Ident]miniast.GoMiniType),
		FieldLocs: make(map[miniast.Ident]*miniast.Position),
		FieldTags: make(map[miniast.Ident]string),
		Doc:       doc,
	}
	for _, field := range s.Fields.List {
		typeName := c.typeToString(field.Type)
		tag := ""
		if field.Tag != nil {
			if unquoted, err := strconv.Unquote(field.Tag.Value); err == nil {
				tag = unquoted
			}
		}
		for _, fieldName := range field.Names {
			ident := miniast.Ident(fieldName.Name)
			res.Fields[ident] = miniast.GoMiniType(typeName)
			res.FieldNames = append(res.FieldNames, ident)
			res.FieldLocs[ident] = c.extractLoc(fieldName)
			if tag != "" {
				res.FieldTags[ident] = tag
			}
		}
	}
	if len(res.FieldTags) == 0 {
		res.FieldTags = nil
	}
	return res
}

func (c *Converter) convertFunc(d *ast.FuncDecl) *miniast.FunctionStmt {
	fnName := d.Name.Name
	var params []miniast.FunctionParam
	var receiverType string

	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		typeName := c.typeToString(recv.Type)
		recvType := miniast.GoMiniType(typeName)
		if elem, ok := recvType.GetPtrElementType(); ok {
			recvType = elem
		}
		receiverType = string(recvType)
		if len(recv.Names) > 0 {
			params = append(params, miniast.FunctionParam{Name: miniast.Ident(recv.Names[0].Name), Type: miniast.GoMiniType(typeName)})
		} else {
			params = append(params, miniast.FunctionParam{Name: "_", Type: miniast.GoMiniType(typeName)})
		}
	}

	var doc string
	if d.Doc != nil {
		doc = d.Doc.Text()
	}
	bodyNode := ast.Node(d)
	if d.Body != nil {
		bodyNode = d.Body
	}

	fn := &miniast.FunctionStmt{
		BaseNode:     miniast.BaseNode{ID: c.genID(d, "function"), Meta: "function", Loc: c.extractLoc(d)},
		Name:         miniast.Ident(fnName),
		ReceiverType: miniast.Ident(receiverType),
		Body:         &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(bodyNode, "block"), Meta: "block", Loc: c.extractLoc(bodyNode)}},
		FunctionType: miniast.FunctionType{Params: params},
		Doc:          doc,
	}
	if d.Type.Params != nil {
		for _, p := range d.Type.Params.List {
			t := c.typeToString(p.Type)
			if _, isVariadic := p.Type.(*ast.Ellipsis); isVariadic {
				fn.Variadic = true
			}
			if len(p.Names) == 0 {
				fn.Params = append(fn.Params, miniast.FunctionParam{Name: "_", Type: miniast.GoMiniType(t)})
			} else {
				for _, name := range p.Names {
					fn.Params = append(fn.Params, miniast.FunctionParam{Name: miniast.Ident(name.Name), Type: miniast.GoMiniType(t)})
				}
			}
		}
	}
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		var returns []miniast.GoMiniType
		for _, r := range d.Type.Results.List {
			returns = append(returns, miniast.GoMiniType(c.typeToString(r.Type)))
		}
		fn.Return = miniast.CreateTupleType(returns...)
	} else {
		fn.Return = "Void"
	}
	if d.Body != nil {
		for _, stmt := range d.Body.List {
			if s := c.convertStmt(stmt); s != nil {
				fn.Body.Children = append(fn.Body.Children, s)
			}
		}
	}
	return fn
}
