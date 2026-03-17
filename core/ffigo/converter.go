package ffigo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	mini_ast "gopkg.d7z.net/go-mini/core/ast"
)

type GoToASTConverter struct {
	fset    *token.FileSet
	imports map[string]string // Alias -> Path
}

func NewGoToASTConverter() *GoToASTConverter {
	return &GoToASTConverter{
		fset:    token.NewFileSet(),
		imports: make(map[string]string),
	}
}

func (c *GoToASTConverter) ConvertSource(code string) (mini_ast.Node, error) {
	f, err := parser.ParseFile(c.fset, "", code, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// 记录导入
	c.imports = make(map[string]string)
	var miniImports []mini_ast.ImportSpec
	for _, imp := range f.Imports {
		path := imp.Path.Value[1 : len(imp.Path.Value)-1]
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			// 简单处理：使用路径最后一段作为包名
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}
		c.imports[alias] = path
		miniImports = append(miniImports, mini_ast.ImportSpec{
			Alias: alias,
			Path:  path,
		})
	}

	program := &mini_ast.ProgramStmt{
		BaseNode:  mini_ast.BaseNode{ID: "boot", Meta: "boot", Type: "Void"},
		Package:   f.Name.Name,
		Constants: make(map[string]string),
		Variables: make(map[mini_ast.Ident]mini_ast.Expr),
		Structs:   make(map[mini_ast.Ident]*mini_ast.StructStmt),
		Functions: make(map[mini_ast.Ident]*mini_ast.FunctionStmt),
		Imports:   miniImports,
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			fn := c.convertFunc(d)
			program.Functions[fn.Name] = fn
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if st, ok := s.Type.(*ast.StructType); ok {
						program.Structs[mini_ast.Ident(s.Name.Name)] = c.convertStruct(s.Name.Name, st)
					}
				case *ast.ValueSpec:
					if d.Tok == token.CONST {
						for i, name := range s.Names {
							if i < len(s.Values) {
								if lit, ok := s.Values[i].(*ast.BasicLit); ok {
									val := lit.Value
									if lit.Kind == token.STRING && len(val) >= 2 {
										val = val[1 : len(val)-1]
									}
									program.Constants[name.Name] = val
								}
							}
						}
					} else if d.Tok == token.VAR {
						for i, name := range s.Names {
							var val mini_ast.Expr
							if i < len(s.Values) {
								val = c.convertExpr(s.Values[i])
							}
							program.Variables[mini_ast.Ident(name.Name)] = val
						}
					}
				}
			}
		}
	}

	return program, nil
}

func (c *GoToASTConverter) convertStruct(name string, s *ast.StructType) *mini_ast.StructStmt {
	res := &mini_ast.StructStmt{
		BaseNode: mini_ast.BaseNode{Meta: "struct"},
		Name:     mini_ast.Ident(name),
		Fields:   make(map[mini_ast.Ident]mini_ast.GoMiniType),
	}
	for _, field := range s.Fields.List {
		typeName := c.typeToString(field.Type)
		for _, fieldName := range field.Names {
			ident := mini_ast.Ident(fieldName.Name)
			res.Fields[ident] = mini_ast.GoMiniType(typeName)
			res.FieldNames = append(res.FieldNames, ident)
		}
	}
	return res
}

func (c *GoToASTConverter) convertFunc(d *ast.FuncDecl) *mini_ast.FunctionStmt {
	fnName := d.Name.Name
	var params []mini_ast.FunctionParam

	// Handle Receiver: func (r T) Name(...) -> __method_T_Name(r T, ...)
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		typeName := c.typeToString(recv.Type)
		// Clean type name
		baseTypeName := strings.TrimPrefix(typeName, "Ptr<")
		baseTypeName = strings.TrimPrefix(baseTypeName, "*")
		baseTypeName = strings.TrimSuffix(baseTypeName, ">")

		fnName = fmt.Sprintf("__method_%s_%s", baseTypeName, fnName)

		if len(recv.Names) > 0 {
			params = append(params, mini_ast.FunctionParam{
				Name: mini_ast.Ident(recv.Names[0].Name),
				Type: mini_ast.GoMiniType(typeName),
			})
		} else {
			params = append(params, mini_ast.FunctionParam{
				Name: "_",
				Type: mini_ast.GoMiniType(typeName),
			})
		}
	}

	fn := &mini_ast.FunctionStmt{
		BaseNode: mini_ast.BaseNode{Meta: "function"},
		Name:     mini_ast.Ident(fnName),
		Body:     &mini_ast.BlockStmt{BaseNode: mini_ast.BaseNode{Meta: "block"}},
		FunctionType: mini_ast.FunctionType{
			Params: params,
		},
	}
	// Params
	if d.Type.Params != nil {
		for _, p := range d.Type.Params.List {
			t := c.typeToString(p.Type)
			for _, name := range p.Names {
				fn.Params = append(fn.Params, mini_ast.FunctionParam{
					Name: mini_ast.Ident(name.Name),
					Type: mini_ast.GoMiniType(t),
				})
			}
		}
	}
	// Return
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		fn.Return = mini_ast.GoMiniType(c.typeToString(d.Type.Results.List[0].Type))
	} else {
		fn.Return = "Void"
	}
	// Body
	if d.Body != nil {
		for _, stmt := range d.Body.List {
			if s := c.convertStmt(stmt); s != nil {
				fn.Body.Children = append(fn.Body.Children, s)
			}
		}
	}
	return fn
}

func (c *GoToASTConverter) convertStmt(s ast.Stmt) mini_ast.Stmt {
	if s == nil {
		return nil
	}
	switch st := s.(type) {
	case *ast.ExprStmt:
		expr := c.convertExpr(st.X)
		if call, ok := expr.(*mini_ast.CallExprStmt); ok {
			return call
		}
		return nil
	case *ast.ReturnStmt:
		res := &mini_ast.ReturnStmt{BaseNode: mini_ast.BaseNode{Meta: "return"}}
		for _, r := range st.Results {
			res.Results = append(res.Results, c.convertExpr(r))
		}
		return res
	case *ast.AssignStmt:
		if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
			return nil
		}
		lhs := st.Lhs[0]
		rhs := st.Rhs[0]

		if st.Tok == token.DEFINE { // :=
			ident, ok := lhs.(*ast.Ident)
			if !ok {
				return nil
			} // := 只能用于标识符
			return &mini_ast.BlockStmt{
				BaseNode: mini_ast.BaseNode{Meta: "block"},
				Inner:    true,
				Children: []mini_ast.Stmt{
					&mini_ast.GenDeclStmt{
						BaseNode: mini_ast.BaseNode{Meta: "decl"},
						Name:     mini_ast.Ident(ident.Name),
						Kind:     "Any",
					},
					&mini_ast.AssignmentStmt{
						BaseNode: mini_ast.BaseNode{Meta: "assignment"},
						LHS:      &mini_ast.IdentifierExpr{BaseNode: mini_ast.BaseNode{Meta: "identifier"}, Name: mini_ast.Ident(ident.Name)},
						Value:    c.convertExpr(rhs),
					},
				},
			}
		}

		if st.Tok == token.ASSIGN {
			return &mini_ast.AssignmentStmt{
				BaseNode: mini_ast.BaseNode{Meta: "assignment"},
				LHS:      c.convertExpr(lhs),
				Value:    c.convertExpr(rhs),
			}
		}
		return nil

	case *ast.DeclStmt:
		if decl, ok := st.Decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
			var children []mini_ast.Stmt
			for _, spec := range decl.Specs {
				if vSpec, ok := spec.(*ast.ValueSpec); ok {
					vType := c.typeToString(vSpec.Type)
					for i, name := range vSpec.Names {
						children = append(children, &mini_ast.GenDeclStmt{
							BaseNode: mini_ast.BaseNode{Meta: "decl"},
							Name:     mini_ast.Ident(name.Name),
							Kind:     mini_ast.GoMiniType(vType),
						})
						if i < len(vSpec.Values) {
							children = append(children, &mini_ast.AssignmentStmt{
								BaseNode: mini_ast.BaseNode{Meta: "assignment"},
								LHS:      &mini_ast.IdentifierExpr{BaseNode: mini_ast.BaseNode{Meta: "identifier"}, Name: mini_ast.Ident(name.Name)},
								Value:    c.convertExpr(vSpec.Values[i]),
							})
						}
					}
				}
			}
			return &mini_ast.BlockStmt{
				BaseNode: mini_ast.BaseNode{Meta: "block"},
				Inner:    true,
				Children: children,
			}
		}
		return nil

	case *ast.IfStmt:
		res := &mini_ast.IfStmt{BaseNode: mini_ast.BaseNode{Meta: "if"}}
		res.Cond = c.convertExpr(st.Cond)
		res.Body = c.toBlock(st.Body)
		if st.Else != nil {
			res.ElseBody = c.toBlock(st.Else)
		}
		return res

	case *ast.ForStmt:
		res := &mini_ast.ForStmt{BaseNode: mini_ast.BaseNode{Meta: "for"}}
		if st.Init != nil {
			res.Init = c.convertStmt(st.Init)
		}
		if st.Cond != nil {
			res.Cond = c.convertExpr(st.Cond)
		}
		if st.Post != nil {
			res.Update = c.convertStmt(st.Post)
		}
		res.Body = c.toBlock(st.Body)
		return res

	case *ast.RangeStmt:
		res := &mini_ast.RangeStmt{BaseNode: mini_ast.BaseNode{Meta: "range"}}
		if st.Key != nil {
			res.Key = mini_ast.Ident(st.Key.(*ast.Ident).Name)
		}
		if st.Value != nil {
			res.Value = mini_ast.Ident(st.Value.(*ast.Ident).Name)
		}
		res.X = c.convertExpr(st.X)
		res.Body = c.toBlock(st.Body)
		res.Define = st.Tok == token.DEFINE
		return res

	case *ast.SwitchStmt:
		res := &mini_ast.SwitchStmt{BaseNode: mini_ast.BaseNode{Meta: "switch"}}
		if st.Init != nil {
			res.Init = c.convertStmt(st.Init)
		}
		if st.Tag != nil {
			res.Tag = c.convertExpr(st.Tag)
		}
		res.Body = &mini_ast.BlockStmt{BaseNode: mini_ast.BaseNode{Meta: "block"}}
		for _, stmt := range st.Body.List {
			if clause, ok := stmt.(*ast.CaseClause); ok {
				cClause := &mini_ast.CaseClause{BaseNode: mini_ast.BaseNode{Meta: "case"}}
				for _, expr := range clause.List {
					cClause.List = append(cClause.List, c.convertExpr(expr))
				}
				for _, bStmt := range clause.Body {
					cClause.Body = append(cClause.Body, c.convertStmt(bStmt))
				}
				res.Body.Children = append(res.Body.Children, cClause)
			}
		}
		return res

	case *ast.DeferStmt:
		call := c.convertExpr(st.Call)
		if cExpr, ok := call.(*mini_ast.CallExprStmt); ok {
			return &mini_ast.DeferStmt{
				BaseNode: mini_ast.BaseNode{Meta: "defer"},
				Call:     cExpr,
			}
		}
		return nil

	case *ast.BlockStmt:
		return c.toBlock(st)

	case *ast.IncDecStmt:
		return &mini_ast.IncDecStmt{
			BaseNode: mini_ast.BaseNode{Meta: "increment"},
			Operand:  c.convertExpr(st.X),
			Operator: mini_ast.Ident(st.Tok.String()),
		}

	case *ast.BranchStmt:
		if st.Tok == token.BREAK || st.Tok == token.CONTINUE {
			return &mini_ast.InterruptStmt{
				BaseNode:      mini_ast.BaseNode{Meta: "interrupt"},
				InterruptType: st.Tok.String(),
			}
		}
	}
	return nil
}

func (c *GoToASTConverter) toBlock(s ast.Stmt) *mini_ast.BlockStmt {
	if b, ok := s.(*ast.BlockStmt); ok {
		res := &mini_ast.BlockStmt{BaseNode: mini_ast.BaseNode{Meta: "block"}}
		for _, item := range b.List {
			if converted := c.convertStmt(item); converted != nil {
				res.Children = append(res.Children, converted)
			}
		}
		return res
	}
	// Wrap single statement
	if converted := c.convertStmt(s); converted != nil {
		return &mini_ast.BlockStmt{BaseNode: mini_ast.BaseNode{Meta: "block"}, Children: []mini_ast.Stmt{converted}}
	}
	return &mini_ast.BlockStmt{BaseNode: mini_ast.BaseNode{Meta: "block"}}
}

func (c *GoToASTConverter) convertExpr(e ast.Expr) mini_ast.Expr {
	if e == nil {
		return nil
	}
	switch ex := e.(type) {
	case *ast.BasicLit:
		t := "String"
		val := ex.Value
		switch ex.Kind {
		case token.INT:
			t = "Int64"
		case token.FLOAT:
			t = "Float64"
		case token.STRING:
			if len(val) >= 2 {
				val = val[1 : len(val)-1]
			}
		}
		return &mini_ast.LiteralExpr{BaseNode: mini_ast.BaseNode{Meta: "literal", Type: mini_ast.GoMiniType(t)}, Value: val}
	case *ast.Ident:
		if ex.Name == "true" || ex.Name == "false" {
			return &mini_ast.LiteralExpr{BaseNode: mini_ast.BaseNode{Meta: "literal", Type: "Bool"}, Value: ex.Name}
		}
		return &mini_ast.IdentifierExpr{BaseNode: mini_ast.BaseNode{Meta: "identifier"}, Name: mini_ast.Ident(ex.Name)}
	case *ast.BinaryExpr:
		return &mini_ast.BinaryExpr{BaseNode: mini_ast.BaseNode{Meta: "binary"}, Left: c.convertExpr(ex.X), Operator: mini_ast.Ident(c.convertOp(ex.Op)), Right: c.convertExpr(ex.Y)}
	case *ast.UnaryExpr:
		return &mini_ast.UnaryExpr{BaseNode: mini_ast.BaseNode{Meta: "unary"}, Operator: mini_ast.Ident(c.convertOp(ex.Op)), Operand: c.convertExpr(ex.X)}
	case *ast.ParenExpr:
		return c.convertExpr(ex.X)
	case *ast.CallExpr:
		funExpr := c.convertExpr(ex.Fun)
		// 如果被调者是一个标识符，将其转换为 ConstRefExpr 以匹配 Validator 预期
		if ident, ok := funExpr.(*mini_ast.IdentifierExpr); ok {
			funExpr = &mini_ast.ConstRefExpr{
				BaseNode: mini_ast.BaseNode{Meta: "const_ref"},
				Name:     ident.Name,
			}
		}
		// 特殊处理类型转换
		if array, ok := ex.Fun.(*ast.ArrayType); ok {
			if ident, ok := array.Elt.(*ast.Ident); ok && ident.Name == "byte" {
				funExpr = &mini_ast.ConstRefExpr{
					BaseNode: mini_ast.BaseNode{Meta: "const_ref"},
					Name:     "[]byte",
				}
			}
		}
		return &mini_ast.CallExprStmt{
			BaseNode: mini_ast.BaseNode{Meta: "call"},
			Func:     funExpr,
			Args:     c.convertArgs(ex.Args),
		}
	case *ast.CompositeLit:
		typeName := c.typeToString(ex.Type)
		res := &mini_ast.CompositeExpr{
			BaseNode: mini_ast.BaseNode{Meta: "composite"},
			Kind:     mini_ast.Ident(typeName),
			Values:   make([]mini_ast.CompositeElement, len(ex.Elts)),
		}
		for i, elt := range ex.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				var keyExpr mini_ast.Expr
				if ident, ok := kv.Key.(*ast.Ident); ok {
					keyExpr = &mini_ast.IdentifierExpr{
						BaseNode: mini_ast.BaseNode{Meta: "identifier"},
						Name:     mini_ast.Ident(ident.Name),
					}
				} else {
					keyExpr = c.convertExpr(kv.Key)
				}
				res.Values[i] = mini_ast.CompositeElement{
					Key:   keyExpr,
					Value: c.convertExpr(kv.Value),
				}
			} else {
				res.Values[i] = mini_ast.CompositeElement{
					Value: c.convertExpr(elt),
				}
			}
		}
		return res
	case *ast.SelectorExpr:
		if xIdent, ok := ex.X.(*ast.Ident); ok {
			// 只有当 X 是已导入的包名时，才视为包成员访问（静态绑定）
			if _, isPkg := c.imports[xIdent.Name]; isPkg {
				return &mini_ast.ConstRefExpr{
					BaseNode: mini_ast.BaseNode{Meta: "const_ref"},
					Name:     mini_ast.Ident(fmt.Sprintf("%s.%s", xIdent.Name, ex.Sel.Name)),
				}
			}
		}
		// 否则视为对象成员访问（动态绑定/Result访问）
		return &mini_ast.MemberExpr{
			BaseNode: mini_ast.BaseNode{Meta: "member"},
			Object:   c.convertExpr(ex.X),
			Property: mini_ast.Ident(ex.Sel.Name),
		}
	case *ast.IndexExpr:
		return &mini_ast.IndexExpr{
			BaseNode: mini_ast.BaseNode{Meta: "index"},
			Object:   c.convertExpr(ex.X),
			Index:    c.convertExpr(ex.Index),
		}
	case *ast.SliceExpr:
		res := &mini_ast.SliceExpr{
			BaseNode: mini_ast.BaseNode{Meta: "slice"},
			X:        c.convertExpr(ex.X),
		}
		if ex.Low != nil {
			res.Low = c.convertExpr(ex.Low)
		}
		if ex.High != nil {
			res.High = c.convertExpr(ex.High)
		}
		return res
	}
	return nil
}

func (c *GoToASTConverter) convertOp(op token.Token) string {
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

func (c *GoToASTConverter) convertArgs(args []ast.Expr) []mini_ast.Expr {
	var res []mini_ast.Expr
	for _, a := range args {
		if ca := c.convertExpr(a); ca != nil {
			res = append(res, ca)
		}
	}
	return res
}

func (c *GoToASTConverter) typeToString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		name := t.Name
		if name == "int" || name == "int64" {
			return "Int64"
		}
		if name == "float64" || name == "float32" {
			return "Float64"
		}
		if name == "string" {
			return "String"
		}
		if name == "bool" {
			return "Bool"
		}
		if name == "any" || name == "interface{}" {
			return "Any"
		}
		return name
	case *ast.ArrayType:
		return fmt.Sprintf("Array<%s>", c.typeToString(t.Elt))
	case *ast.StarExpr:
		return fmt.Sprintf("Ptr<%s>", c.typeToString(t.X))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", c.typeToString(t.Key), c.typeToString(t.Value))
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", c.typeToString(t.X), t.Sel.Name)
	}
	return "Any"
}
