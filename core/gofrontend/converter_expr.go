package gofrontend

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func (c *Converter) convertExpr(e ast.Expr) miniast.Expr {
	if e == nil {
		return nil
	}
	switch ex := e.(type) {
	case *ast.BadExpr:
		return c.badExpr(ex, "无法解析的表达式")
	case *ast.BasicLit:
		t := string(miniast.TypeString)
		val := ex.Value
		switch ex.Kind {
		case token.INT:
			t = string(miniast.TypeInt64)
		case token.FLOAT:
			t = string(miniast.TypeFloat64)
		case token.STRING:
			if len(val) >= 2 {
				if unquoted, err := strconv.Unquote(val); err == nil {
					val = unquoted
				} else {
					val = val[1 : len(val)-1]
				}
			}
		}
		return &miniast.LiteralExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "literal"), Meta: "literal", Type: miniast.GoMiniType(t), Loc: c.extractLoc(ex)}, Value: val}
	case *ast.Ident:
		if ex.Name == "true" || ex.Name == "false" {
			return &miniast.LiteralExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "literal"), Meta: "literal", Type: miniast.TypeBool, Loc: c.extractLoc(ex)}, Value: ex.Name}
		}
		switch ex.Name {
		case "panic", "make", "append", "delete", "len", "require":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex)}, Name: miniast.Ident(ex.Name)}
		}
		if builtin := c.canonicalBuiltinTypeName(ex.Name); builtin != ex.Name {
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex)}, Name: miniast.Ident(builtin)}
		}
		return &miniast.IdentifierExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "identifier"), Meta: "identifier", Loc: c.extractLoc(ex)}, Name: miniast.Ident(ex.Name)}
	case *ast.BinaryExpr:
		return &miniast.BinaryExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "binary"), Meta: "binary", Loc: c.extractLoc(ex)}, Left: c.convertExpr(ex.X), Operator: miniast.Ident(c.convertOp(ex.Op)), Right: c.convertExpr(ex.Y)}
	case *ast.UnaryExpr:
		return &miniast.UnaryExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "unary"), Meta: "unary", Loc: c.extractLoc(ex)}, Operator: miniast.Ident(c.convertOp(ex.Op)), Operand: c.convertExpr(ex.X)}
	case *ast.TypeAssertExpr:
		return &miniast.TypeAssertExpr{
			BaseNode: miniast.BaseNode{
				ID:   c.genID(ex, "assert"),
				Meta: "assert",
				Loc:  c.extractLoc(ex),
			},
			X:    c.convertExpr(ex.X),
			Type: miniast.GoMiniType(c.typeToString(ex.Type)),
		}
	case *ast.ParenExpr:
		return c.convertExpr(ex.X)
	case *ast.CallExpr:
		var funExpr miniast.Expr
		if array, ok := ex.Fun.(*ast.ArrayType); ok {
			if ident, ok := array.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
				funExpr = &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex.Fun, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex.Fun)}, Name: miniast.Ident(miniast.TypeBytes)}
			}
		}
		if funExpr == nil {
			funExpr = c.convertExpr(ex.Fun)
		}
		if ident, ok := funExpr.(*miniast.IdentifierExpr); ok {
			funExpr = &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex.Fun, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex.Fun)}, Name: ident.Name}
		}
		if ident, ok := funExpr.(*miniast.ConstRefExpr); ok {
			switch ident.Name {
			case "make", "new":
				if len(ex.Args) == 0 {
					return c.badExpr(ex, string(ident.Name)+" 至少需要一个类型参数")
				}
				// 严格检测：Go 语言中 new/make 的第一个参数必须是类型标识符，不能是值字面量
				if _, isLit := ex.Args[0].(*ast.BasicLit); isLit {
					return c.badExpr(ex.Args[0], string(ident.Name)+" 第一个参数必须是类型，不能是字面量")
				}

				typeArg := c.typeToString(ex.Args[0])
				args := []miniast.Expr{&miniast.LiteralExpr{
					BaseNode: miniast.BaseNode{
						ID:   c.genID(ex.Args[0], "literal"),
						Meta: "literal",
						Type: miniast.TypeString,
						Loc:  c.extractLoc(ex.Args[0]),
					},
					Value: typeArg,
				}}
				args = append(args, c.convertArgs(ex.Args[1:])...)
				return &miniast.CallExprStmt{BaseNode: miniast.BaseNode{ID: c.genID(ex, "call"), Meta: "call", Loc: c.extractLoc(ex)}, Func: funExpr, Args: args}
			case "require":
				if len(ex.Args) == 1 {
					if lit, ok := ex.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						val := lit.Value
						path := ""
						if len(val) >= 2 {
							if unquoted, err := strconv.Unquote(val); err == nil {
								path = unquoted
							} else {
								path = val[1 : len(val)-1]
							}
						}
						return &miniast.ImportExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "import"), Meta: "import", Type: miniast.TypeModule}, Path: path}
					}
				}
			}
		}
		return &miniast.CallExprStmt{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "call"), Meta: "call", Loc: c.extractLoc(ex)},
			Func:     funExpr,
			Args:     c.convertArgs(ex.Args),
			Ellipsis: ex.Ellipsis.IsValid(),
		}
	case *ast.CompositeLit:
		typeName := c.typeToString(ex.Type)
		res := &miniast.CompositeExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "composite"), Meta: "composite", Loc: c.extractLoc(ex)}, Kind: miniast.Ident(typeName), Values: make([]miniast.CompositeElement, len(ex.Elts))}
		for i, elt := range ex.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				var keyExpr miniast.Expr
				if ident, ok := kv.Key.(*ast.Ident); ok {
					keyExpr = &miniast.IdentifierExpr{BaseNode: miniast.BaseNode{ID: c.genID(ident, "identifier"), Meta: "identifier", Loc: c.extractLoc(ident)}, Name: miniast.Ident(ident.Name)}
				} else {
					keyExpr = c.convertExpr(kv.Key)
				}
				res.Values[i] = miniast.CompositeElement{Key: keyExpr, Value: c.convertExpr(kv.Value)}
			} else {
				res.Values[i] = miniast.CompositeElement{Value: c.convertExpr(elt)}
			}
		}
		return res
	case *ast.SelectorExpr:
		return &miniast.MemberExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "member"), Meta: "member", Loc: c.extractLoc(ex)}, Object: c.convertExpr(ex.X), Property: miniast.Ident(ex.Sel.Name)}
	case *ast.IndexExpr:
		return &miniast.IndexExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "index"), Meta: "index", Loc: c.extractLoc(ex)}, Object: c.convertExpr(ex.X), Index: c.convertExpr(ex.Index)}
	case *ast.SliceExpr:
		res := &miniast.SliceExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "slice"), Meta: "slice", Loc: c.extractLoc(ex)}, X: c.convertExpr(ex.X)}
		if ex.Low != nil {
			res.Low = c.convertExpr(ex.Low)
		}
		if ex.High != nil {
			res.High = c.convertExpr(ex.High)
		}
		return res
	case *ast.StarExpr:
		return &miniast.StarExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "star"), Meta: "star", Loc: c.extractLoc(ex)}, X: c.convertExpr(ex.X)}
	case *ast.Ellipsis:
		return c.convertExpr(ex.Elt)
	case *ast.FuncLit:
		var params []miniast.FunctionParam
		if ex.Type.Params != nil {
			for _, field := range ex.Type.Params.List {
				typeName := c.typeToString(field.Type)
				if len(field.Names) == 0 {
					params = append(params, miniast.FunctionParam{Type: miniast.GoMiniType(typeName)})
				} else {
					for _, name := range field.Names {
						params = append(params, miniast.FunctionParam{Name: miniast.Ident(name.Name), Type: miniast.GoMiniType(typeName)})
					}
				}
			}
		}
		retType := miniast.TypeVoid
		if ex.Type.Results != nil {
			var results []miniast.GoMiniType
			for _, r := range ex.Type.Results.List {
				results = append(results, miniast.GoMiniType(c.typeToString(r.Type)))
			}
			if len(results) > 0 {
				retType = miniast.CreateTupleType(results...)
			}
		}
		funcExpr := &miniast.FuncLitExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "func_lit"), Meta: "func_lit", Loc: c.extractLoc(ex)}, FunctionType: miniast.FunctionType{Params: params, Return: retType}}
		if ex.Body != nil {
			body := c.convertStmt(ex.Body)
			block, ok := body.(*miniast.BlockStmt)
			if !ok {
				funcExpr.Body = &miniast.BlockStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(ex.Body, "block"), Meta: "block", Loc: c.extractLoc(ex.Body), InvalidCause: "函数 literal 主体无法转换为 block"},
					Inner:    true,
					Children: []miniast.Stmt{c.badStmt(ex.Body, "函数 literal 主体无法转换为 block")},
				}
				return funcExpr
			}
			funcExpr.Body = block
			funcExpr.Body.Inner = true
		}
		return funcExpr
	}
	return c.badExpr(e, fmt.Sprintf("不支持的表达式: %T", e))
}

func (c *Converter) convertOp(op token.Token) string {
	switch op {
	case token.ADD:
		return "Plus"
	case token.SUB:
		return "Minus"
	case token.MUL:
		return "Mult"
	case token.QUO:
		return "Div"
	case token.REM:
		return "Mod"
	case token.EQL:
		return "Eq"
	case token.NEQ:
		return "Neq"
	case token.LSS:
		return "Lt"
	case token.GTR:
		return "Gt"
	case token.LEQ:
		return "Le"
	case token.GEQ:
		return "Ge"
	case token.LAND:
		return "And"
	case token.LOR:
		return "Or"
	case token.NOT:
		return "Not"
	case token.AND:
		return "BitAnd"
	case token.OR:
		return "BitOr"
	case token.XOR:
		return "BitXor"
	case token.SHL:
		return "Lsh"
	case token.SHR:
		return "Rsh"
	}
	return op.String()
}

func (c *Converter) convertArgs(args []ast.Expr) []miniast.Expr {
	var res []miniast.Expr
	for _, a := range args {
		if ca := c.convertExpr(a); ca != nil {
			res = append(res, ca)
		}
	}
	return res
}
