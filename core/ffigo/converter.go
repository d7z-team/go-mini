package ffigo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"hash/fnv"
	"reflect"
	"strconv"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

type GoToASTConverter struct {
	fset       *token.FileSet
	imports    map[string]string // Alias -> Path
	interfaces map[string]*ast.InterfaceType
}

func (c *GoToASTConverter) genID(node ast.Node, meta string) string {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Ptr && reflect.ValueOf(node).IsNil()) {
		return "meta_" + meta
	}
	pos := c.fset.Position(node.Pos())
	h := fnv.New64a()
	// Using string concatenation is much faster than fmt.Fprintf for simple strings
	posStr := pos.Filename + ":" + strconv.Itoa(pos.Line) + ":" + strconv.Itoa(pos.Column) + ":" + meta
	h.Write([]byte(posStr))
	return strconv.FormatUint(h.Sum64(), 16)
}

func (c *GoToASTConverter) extractLoc(node ast.Node) *miniast.Position {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Ptr && reflect.ValueOf(node).IsNil()) || c.fset == nil {
		return nil
	}
	start := c.fset.Position(node.Pos())
	if start.Line == 0 {
		return nil
	}
	end := c.fset.Position(node.End())
	return &miniast.Position{
		F:  start.Filename,
		L:  start.Line,
		C:  start.Column,
		EL: end.Line,
		EC: end.Column,
	}
}

func NewGoToASTConverter() *GoToASTConverter {
	return &GoToASTConverter{
		fset:       token.NewFileSet(),
		imports:    make(map[string]string),
		interfaces: make(map[string]*ast.InterfaceType),
	}
}

func (c *GoToASTConverter) ConvertSource(filename, code string) (miniast.Node, error) {
	node, errs := c.convert(filename, code, false)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return node, nil
}

func (c *GoToASTConverter) ConvertSourceTolerant(filename, code string) (miniast.Node, []error) {
	return c.convert(filename, code, true)
}

func (c *GoToASTConverter) convert(filename, code string, tolerant bool) (miniast.Node, []error) {
	mode := parser.ParseComments
	if tolerant {
		mode |= parser.AllErrors
	}
	f, err := parser.ParseFile(c.fset, filename, code, mode)
	var errs []error
	if err != nil {
		if f == nil && !tolerant {
			return nil, []error{err}
		}
		if list, ok := err.(scanner.ErrorList); ok {
			for _, e := range list {
				errs = append(errs, e)
			}
		} else {
			errs = append(errs, err)
		}
	}

	// 记录导入
	c.imports = make(map[string]string)
	var miniImports []miniast.ImportSpec
	if f != nil {
		for _, imp := range f.Imports {
			if len(imp.Path.Value) < 2 {
				continue
			}
			path := imp.Path.Value
			if unquoted, err := strconv.Unquote(path); err == nil {
				path = unquoted
			} else {
				path = path[1 : len(path)-1]
			}
			var alias string
			if imp.Name != nil {
				alias = imp.Name.Name
			} else {
				parts := strings.Split(path, "/")
				alias = parts[len(parts)-1]
			}
			c.imports[alias] = path
			miniImports = append(miniImports, miniast.ImportSpec{
				Alias: alias,
				Path:  path,
			})
		}
	}

	program := &miniast.ProgramStmt{
		BaseNode:   miniast.BaseNode{ID: c.genID(f, "boot"), Meta: "boot", Type: "Void", Loc: c.extractLoc(f)},
		Constants:  make(map[string]string),
		Variables:  make(map[miniast.Ident]miniast.Expr),
		Types:      make(map[miniast.Ident]miniast.GoMiniType),
		Structs:    make(map[miniast.Ident]*miniast.StructStmt),
		Interfaces: make(map[miniast.Ident]*miniast.InterfaceStmt),
		Functions:  make(map[miniast.Ident]*miniast.FunctionStmt),
		Imports:    miniImports,
	}
	if f != nil {
		program.Package = f.Name.Name
	}

	for i, imp := range miniImports {
		program.Variables[miniast.Ident(imp.Alias)] = &miniast.ImportExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(f.Imports[i], "import"), Meta: "import", Type: miniast.TypeModule},
			Path:     imp.Path,
		}
	}

	if f != nil {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				fn := c.convertFunc(d)
				key := fn.Name
				if fn.ReceiverType != "" {
					key = miniast.Ident(string(fn.ReceiverType) + "." + string(fn.Name))
				}
				program.Functions[key] = fn
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if st, ok := s.Type.(*ast.StructType); ok {
							var doc string
							if s.Doc != nil {
								doc = s.Doc.Text()
							} else if d.Doc != nil {
								doc = d.Doc.Text()
							}
							program.Structs[miniast.Ident(s.Name.Name)] = c.convertStruct(s.Name.Name, st, doc)
						} else if it, ok := s.Type.(*ast.InterfaceType); ok {
							c.interfaces[s.Name.Name] = it // 记录接口定义
							program.Interfaces[miniast.Ident(s.Name.Name)] = &miniast.InterfaceStmt{
								BaseNode: miniast.BaseNode{
									ID:   c.genID(s, "interface"),
									Meta: "interface",
									Type: "Void",
									Loc:  c.extractLoc(s),
								},
								Name: miniast.Ident(s.Name.Name),
								Type: miniast.GoMiniType(c.typeToString(it)),
							}
						} else {
							// 基础类型别名 (type MyInt int64)
							program.Types[miniast.Ident(s.Name.Name)] = miniast.GoMiniType(c.typeToString(s.Type))
						}
					case *ast.ValueSpec:
						switch d.Tok {
						case token.CONST:
							for i, name := range s.Names {
								if i < len(s.Values) {
									if lit, ok := s.Values[i].(*ast.BasicLit); ok {
										val := lit.Value
										if lit.Kind == token.STRING && len(val) >= 2 {
											if unquoted, err := strconv.Unquote(val); err == nil {
												val = unquoted
											} else {
												val = val[1 : len(val)-1]
											}
										}
										program.Constants[name.Name] = val
									}
								}
							}
						case token.VAR:
							for i, name := range s.Names {
								var val miniast.Expr
								if i < len(s.Values) {
									val = c.convertExpr(s.Values[i])
								}
								program.Variables[miniast.Ident(name.Name)] = val

								// 物理落盘：将声明作为语句添加到 Main 中，以便 FindNodeAt 能够命中
								decl := &miniast.GenDeclStmt{
									BaseNode: miniast.BaseNode{
										ID:   c.genID(name, "decl"),
										Meta: "decl",
										Loc:  c.extractLoc(name),
									},
									Name: miniast.Ident(name.Name),
									Kind: miniast.GoMiniType(c.typeToString(s.Type)),
								}
								program.Main = append(program.Main, decl)
							}
						}
					}
				}
			}
		}
	}

	return program, errs
}

func (c *GoToASTConverter) ConvertExprSource(code string) (miniast.Expr, error) {
	e, err := parser.ParseExpr(code)
	if err != nil {
		return nil, err
	}
	return c.convertExpr(e), nil
}

func (c *GoToASTConverter) ConvertStmtsSource(code string) ([]miniast.Stmt, error) {
	wrapper := fmt.Sprintf("package main\nfunc main() {\n%s\n}", code)
	node, err := c.ConvertSource("snippet", wrapper)
	if err != nil {
		return nil, err
	}
	prog := node.(*miniast.ProgramStmt)
	if len(prog.Main) > 0 {
		return prog.Main, nil
	}
	if mainFunc, ok := prog.Functions["main"]; ok && mainFunc.Body != nil {
		return mainFunc.Body.Children, nil
	}
	return nil, nil
}

func (c *GoToASTConverter) convertStruct(name string, s *ast.StructType, doc string) *miniast.StructStmt {
	res := &miniast.StructStmt{
		BaseNode: miniast.BaseNode{ID: c.genID(s, "struct"), Meta: "struct", Loc: c.extractLoc(s)},
		Name:     miniast.Ident(name),
		Fields:   make(map[miniast.Ident]miniast.GoMiniType),
		Doc:      doc,
	}
	for _, field := range s.Fields.List {
		typeName := c.typeToString(field.Type)
		for _, fieldName := range field.Names {
			ident := miniast.Ident(fieldName.Name)
			res.Fields[ident] = miniast.GoMiniType(typeName)
			res.FieldNames = append(res.FieldNames, ident)
		}
	}
	return res
}

func (c *GoToASTConverter) convertFunc(d *ast.FuncDecl) *miniast.FunctionStmt {
	fnName := d.Name.Name
	var params []miniast.FunctionParam
	var receiverType string

	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		typeName := c.typeToString(recv.Type)
		baseTypeName := strings.TrimPrefix(typeName, "Ptr<")
		baseTypeName = strings.TrimPrefix(baseTypeName, "*")
		baseTypeName = strings.TrimSuffix(baseTypeName, ">")
		receiverType = baseTypeName
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

	fn := &miniast.FunctionStmt{
		BaseNode:     miniast.BaseNode{ID: c.genID(d, "function"), Meta: "function", Loc: c.extractLoc(d)},
		Name:         miniast.Ident(fnName),
		ReceiverType: miniast.Ident(receiverType),
		Body:         &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(d.Body, "block"), Meta: "block", Loc: c.extractLoc(d.Body)}},
		FunctionType: miniast.FunctionType{Params: params},
		Doc:          doc,
	}
	if d.Type.Params != nil {
		for _, p := range d.Type.Params.List {
			t := c.typeToString(p.Type)
			if _, isVariadic := p.Type.(*ast.Ellipsis); isVariadic {
				fn.Variadic = true
			}
			for _, name := range p.Names {
				fn.Params = append(fn.Params, miniast.FunctionParam{Name: miniast.Ident(name.Name), Type: miniast.GoMiniType(t)})
			}
		}
	}
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		var returns []string
		for _, r := range d.Type.Results.List {
			returns = append(returns, c.typeToString(r.Type))
		}
		if len(returns) > 1 {
			fn.Return = miniast.GoMiniType(fmt.Sprintf("tuple(%s)", strings.Join(returns, ", ")))
		} else {
			fn.Return = miniast.GoMiniType(returns[0])
		}
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

func (c *GoToASTConverter) convertStmt(s ast.Stmt) miniast.Stmt {
	if s == nil {
		return nil
	}
	switch st := s.(type) {
	case *ast.BadStmt:
		return &miniast.BadStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "bad_stmt"), Meta: "bad_stmt", Loc: c.extractLoc(st)}}
	case *ast.ExprStmt:
		expr := c.convertExpr(st.X)
		if call, ok := expr.(*miniast.CallExprStmt); ok {
			return call
		}
		return &miniast.ExpressionStmt{
			BaseNode: miniast.BaseNode{ID: c.genID(st, "expr_stmt"), Meta: "expr_stmt", Loc: c.extractLoc(st)},
			X:        expr,
		}
	case *ast.ReturnStmt:
		res := &miniast.ReturnStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "return"), Meta: "return", Loc: c.extractLoc(st)}}
		for _, r := range st.Results {
			res.Results = append(res.Results, c.convertExpr(r))
		}
		return res
	case *ast.AssignStmt:
		if st.Tok == token.DEFINE {
			var children []miniast.Stmt
			var lhsExprs []miniast.Expr

			// 尝试推导右值类型
			inferredType := "Any"
			if len(st.Rhs) == 1 {
				if comp, ok := st.Rhs[0].(*ast.CompositeLit); ok {
					fullType := miniast.GoMiniType(c.typeToString(comp.Type))
					if len(st.Lhs) > 1 {
						// 多重赋值：如果右值是容器，则推导元素类型
						if fullType.IsArray() {
							if elem, ok := fullType.ReadArrayItemType(); ok {
								inferredType = string(elem)
							}
						} else if fullType.IsMap() {
							if _, val, ok := fullType.GetMapKeyValueTypes(); ok {
								inferredType = string(val)
							}
						}
					} else {
						inferredType = string(fullType)
					}
				}
			}

			for _, lhs := range st.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					if ident.Name != "_" {
						children = append(children, &miniast.GenDeclStmt{
							BaseNode: miniast.BaseNode{ID: c.genID(lhs, "decl"), Meta: "decl", Loc: c.extractLoc(lhs)},
							Name:     miniast.Ident(ident.Name),
							Kind:     miniast.GoMiniType(inferredType),
						})
						lhsExprs = append(lhsExprs, &miniast.IdentifierExpr{
							BaseNode: miniast.BaseNode{ID: c.genID(lhs, "identifier"), Meta: "identifier", Loc: c.extractLoc(lhs)},
							Name:     miniast.Ident(ident.Name),
						})
					} else {
						// Skip evaluation for blank identifier
						lhsExprs = append(lhsExprs, nil)
					}
				}
			}

			var rhsExpr miniast.Expr
			if len(st.Rhs) == 1 {
				rhsExpr = c.convertExpr(st.Rhs[0])
				if len(st.Lhs) == 2 {
					if ta, ok := rhsExpr.(*miniast.TypeAssertExpr); ok {
						ta.Multi = true
					} else if ie, ok := rhsExpr.(*miniast.IndexExpr); ok {
						ie.Multi = true
					}
				}
			} else {
				// Create a composite expr for multiple RHS values
				comp := &miniast.CompositeExpr{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "rhs_composite"), Meta: "composite", Loc: c.extractLoc(st)},
					Kind:     "Array<Any>",
				}
				for _, r := range st.Rhs {
					comp.Values = append(comp.Values, miniast.CompositeElement{Value: c.convertExpr(r)})
				}
				rhsExpr = comp
			}

			if len(lhsExprs) == 1 {
				children = append(children, &miniast.AssignmentStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
					LHS:      lhsExprs[0],
					Value:    rhsExpr,
				})
			} else {
				children = append(children, &miniast.MultiAssignmentStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "multi_assignment"), Meta: "multi_assignment", Loc: c.extractLoc(st)},
					LHS:      lhsExprs,
					Value:    rhsExpr,
				})
			}
			return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)}, Inner: true, Children: children}
		}
		if st.Tok == token.ASSIGN {
			var rhsExpr miniast.Expr
			if len(st.Rhs) == 1 {
				rhsExpr = c.convertExpr(st.Rhs[0])
				if len(st.Lhs) == 2 {
					if ta, ok := rhsExpr.(*miniast.TypeAssertExpr); ok {
						ta.Multi = true
					} else if ie, ok := rhsExpr.(*miniast.IndexExpr); ok {
						ie.Multi = true
					}
				}
			} else {
				comp := &miniast.CompositeExpr{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "rhs_composite"), Meta: "composite", Loc: c.extractLoc(st)},
					Kind:     "Array<Any>",
				}
				for _, r := range st.Rhs {
					comp.Values = append(comp.Values, miniast.CompositeElement{Value: c.convertExpr(r)})
				}
				rhsExpr = comp
			}

			if len(st.Lhs) == 1 {
				return &miniast.AssignmentStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)}, LHS: c.convertExpr(st.Lhs[0]), Value: rhsExpr}
			}
			var lhsExprs []miniast.Expr
			for _, l := range st.Lhs {
				lhsExprs = append(lhsExprs, c.convertExpr(l))
			}
			return &miniast.MultiAssignmentStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "multi_assignment"), Meta: "multi_assignment", Loc: c.extractLoc(st)}, LHS: lhsExprs, Value: rhsExpr}
		}
		var op token.Token
		switch st.Tok {
		case token.ADD_ASSIGN:
			op = token.ADD
		case token.SUB_ASSIGN:
			op = token.SUB
		case token.MUL_ASSIGN:
			op = token.MUL
		case token.QUO_ASSIGN:
			op = token.QUO
		default:
			return nil
		}
		if len(st.Lhs) == 1 && len(st.Rhs) == 1 {
			lhs := c.convertExpr(st.Lhs[0])
			return &miniast.AssignmentStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
				LHS:      lhs,
				Value: &miniast.BinaryExpr{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "binary"), Meta: "binary", Loc: c.extractLoc(st)},
					Left:     lhs, Operator: miniast.Ident(c.convertOp(op)), Right: c.convertExpr(st.Rhs[0]),
				},
			}
		}
		return nil
	case *ast.DeclStmt:
		if decl, ok := st.Decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
			var children []miniast.Stmt
			for _, spec := range decl.Specs {
				if vSpec, ok := spec.(*ast.ValueSpec); ok {
					vType := c.typeToString(vSpec.Type)
					for i, name := range vSpec.Names {
						children = append(children, &miniast.GenDeclStmt{
							BaseNode: miniast.BaseNode{ID: c.genID(name, "decl"), Meta: "decl", Loc: c.extractLoc(name)},
							Name:     miniast.Ident(name.Name), Kind: miniast.GoMiniType(vType),
						})
						if i < len(vSpec.Values) {
							children = append(children, &miniast.AssignmentStmt{
								BaseNode: miniast.BaseNode{ID: c.genID(name, "assignment"), Meta: "assignment", Loc: c.extractLoc(name)},
								LHS:      &miniast.IdentifierExpr{BaseNode: miniast.BaseNode{ID: c.genID(name, "identifier"), Meta: "identifier", Loc: c.extractLoc(name)}, Name: miniast.Ident(name.Name)},
								Value:    c.convertExpr(vSpec.Values[i]),
							})
						}
					}
				}
			}
			return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)}, Inner: true, Children: children}
		}
		return nil
	case *ast.IfStmt:
		res := &miniast.IfStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "if"), Meta: "if", Loc: c.extractLoc(st)}}
		res.Cond = c.convertExpr(st.Cond)
		res.Body = c.toBlock(st.Body)
		if st.Else != nil {
			res.ElseBody = c.toBlock(st.Else)
		}
		if st.Init != nil {
			return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)}, Inner: false, Children: []miniast.Stmt{c.convertStmt(st.Init), res}}
		}
		return res
	case *ast.ForStmt:
		res := &miniast.ForStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "for"), Meta: "for", Loc: c.extractLoc(st)}}
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
		res := &miniast.RangeStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "range"), Meta: "range", Loc: c.extractLoc(st)}}
		if st.Key != nil {
			res.Key = miniast.Ident(st.Key.(*ast.Ident).Name)
		}
		if st.Value != nil {
			res.Value = miniast.Ident(st.Value.(*ast.Ident).Name)
		}
		res.X = c.convertExpr(st.X)
		res.Body = c.toBlock(st.Body)
		res.Define = st.Tok == token.DEFINE
		return res
	case *ast.SwitchStmt:
		res := &miniast.SwitchStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "switch"), Meta: "switch", Loc: c.extractLoc(st)}}
		if st.Init != nil {
			res.Init = c.convertStmt(st.Init)
		}
		if st.Tag != nil {
			res.Tag = c.convertExpr(st.Tag)
		}
		res.Body = &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st.Body, "block"), Meta: "block", Loc: c.extractLoc(st.Body)}}
		for _, stmt := range st.Body.List {
			if clause, ok := stmt.(*ast.CaseClause); ok {
				cClause := &miniast.CaseClause{BaseNode: miniast.BaseNode{ID: c.genID(clause, "case"), Meta: "case", Loc: c.extractLoc(clause)}}
				for _, expr := range clause.List {
					cClause.List = append(cClause.List, c.convertExpr(expr))
				}
				for _, bStmt := range clause.Body {
					cClause.Body = append(cClause.Body, c.convertStmt(bStmt))
				}
				res.Body.Children = append(res.Body.Children, cClause)
			}
		}
		if st.Init != nil {
			return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)}, Inner: false, Children: []miniast.Stmt{c.convertStmt(st.Init), res}}
		}
		return res
	case *ast.TypeSwitchStmt:
		res := &miniast.SwitchStmt{
			BaseNode: miniast.BaseNode{ID: c.genID(st, "type_switch"), Meta: "switch", Loc: c.extractLoc(st)},
			IsType:   true,
		}
		if st.Init != nil {
			res.Init = c.convertStmt(st.Init)
		}

		// 处理 v := x.(type)
		if st.Assign != nil {
			switch ass := st.Assign.(type) {
			case *ast.AssignStmt:
				// v := x.(type)
				if len(ass.Lhs) == 1 && len(ass.Rhs) == 1 {
					// 提取 x
					if typeAssert, ok := ass.Rhs[0].(*ast.TypeAssertExpr); ok {
						res.Tag = c.convertExpr(typeAssert.X)
						// 构造赋值语句 v := x
						// 注意：由于是 Type Switch，v 的实际类型在每个 case 中可能不同，
						// 目前我们简单地把它声明为 Any。
						res.Assign = c.convertStmt(&ast.AssignStmt{
							Lhs: ass.Lhs,
							Tok: ass.Tok,
							Rhs: []ast.Expr{typeAssert.X},
						})
					}
				}
			case *ast.ExprStmt:
				// x.(type) 不带赋值
				if typeAssert, ok := ass.X.(*ast.TypeAssertExpr); ok {
					res.Tag = c.convertExpr(typeAssert.X)
				}
			}
		}

		res.Body = &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st.Body, "block"), Meta: "block", Loc: c.extractLoc(st.Body)}}
		for _, stmt := range st.Body.List {
			if clause, ok := stmt.(*ast.CaseClause); ok {
				cClause := &miniast.CaseClause{BaseNode: miniast.BaseNode{ID: c.genID(clause, "case"), Meta: "case", Loc: c.extractLoc(clause)}}
				for _, expr := range clause.List {
					// Type Switch 的 Case List 是类型名
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
		if cExpr, ok := call.(*miniast.CallExprStmt); ok {
			return &miniast.DeferStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "defer"), Meta: "defer", Loc: c.extractLoc(st)}, Call: cExpr}
		}
		return nil
	case *ast.BlockStmt:
		return c.toBlock(st)
	case *ast.IncDecStmt:
		return &miniast.IncDecStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "increment"), Meta: "increment", Loc: c.extractLoc(st)}, Operand: c.convertExpr(st.X), Operator: miniast.Ident(st.Tok.String())}
	case *ast.BranchStmt:
		if st.Tok == token.BREAK || st.Tok == token.CONTINUE {
			return &miniast.InterruptStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "interrupt"), Meta: "interrupt", Loc: c.extractLoc(st)}, InterruptType: st.Tok.String()}
		}
	}
	return nil
}

func (c *GoToASTConverter) toBlock(s ast.Stmt) *miniast.BlockStmt {
	if b, ok := s.(*ast.BlockStmt); ok {
		res := &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(b, "block"), Meta: "block", Loc: c.extractLoc(b)}}
		for _, item := range b.List {
			if converted := c.convertStmt(item); converted != nil {
				res.Children = append(res.Children, converted)
			}
		}
		return res
	}
	if converted := c.convertStmt(s); converted != nil {
		return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(s, "block"), Meta: "block", Loc: c.extractLoc(s)}, Children: []miniast.Stmt{converted}}
	}
	return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(s, "block"), Meta: "block", Loc: c.extractLoc(s)}}
}

func (c *GoToASTConverter) convertExpr(e ast.Expr) miniast.Expr {
	if e == nil {
		return nil
	}
	switch ex := e.(type) {
	case *ast.BadExpr:
		return &miniast.BadExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "bad_expr"), Meta: "bad_expr", Loc: c.extractLoc(ex)}}
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
			return &miniast.LiteralExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "literal"), Meta: "literal", Type: "Bool", Loc: c.extractLoc(ex)}, Value: ex.Name}
		}
		switch ex.Name {
		case "panic", "make", "append", "delete", "len", "require":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex)}, Name: miniast.Ident(ex.Name)}
		case "int", "int64":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex)}, Name: "Int64"}
		case "float64":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex)}, Name: "Float64"}
		case "string":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex)}, Name: "String"}
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
		funExpr := c.convertExpr(ex.Fun)
		if ident, ok := funExpr.(*miniast.IdentifierExpr); ok {
			funExpr = &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex.Fun, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex.Fun)}, Name: ident.Name}
		}
		if array, ok := ex.Fun.(*ast.ArrayType); ok {
			if ident, ok := array.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
				funExpr = &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex.Fun, "const_ref"), Meta: "const_ref", Loc: c.extractLoc(ex.Fun)}, Name: "TypeBytes"}
			}
		}
		if ident, ok := funExpr.(*miniast.ConstRefExpr); ok {
			switch ident.Name {
			case "make", "new":
				if len(ex.Args) > 0 {
					// 严格检测：Go 语言中 new/make 的第一个参数必须是类型标识符，不能是值字面量
					if _, isLit := ex.Args[0].(*ast.BasicLit); isLit {
						panic(fmt.Errorf("%s 第一个参数必须是类型，不能是字符串字面量", ident.Name))
					}

					typeArg := c.typeToString(ex.Args[0])
					args := []miniast.Expr{&miniast.LiteralExpr{
						BaseNode: miniast.BaseNode{
							ID:   c.genID(ex.Args[0], "literal"),
							Meta: "literal",
							Type: "String",
							Loc:  c.extractLoc(ex.Args[0]),
						},
						Value: typeArg,
					}}
					args = append(args, c.convertArgs(ex.Args[1:])...)
					return &miniast.CallExprStmt{BaseNode: miniast.BaseNode{ID: c.genID(ex, "call"), Meta: "call", Loc: c.extractLoc(ex)}, Func: funExpr, Args: args}
				}
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
		retType := "Void"
		if ex.Type.Results != nil {
			var results []string
			for _, r := range ex.Type.Results.List {
				results = append(results, c.typeToString(r.Type))
			}
			if len(results) > 1 {
				retType = "tuple(" + strings.Join(results, ", ") + ")"
			} else if len(results) == 1 {
				retType = results[0]
			}
		}
		funcExpr := &miniast.FuncLitExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "func_lit"), Meta: "func_lit", Loc: c.extractLoc(ex)}, FunctionType: miniast.FunctionType{Params: params, Return: miniast.GoMiniType(retType)}}
		if ex.Body != nil {
			funcExpr.Body = c.convertStmt(ex.Body).(*miniast.BlockStmt)
			funcExpr.Body.Inner = true
		}
		return funcExpr
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

func (c *GoToASTConverter) convertArgs(args []ast.Expr) []miniast.Expr {
	var res []miniast.Expr
	for _, a := range args {
		if ca := c.convertExpr(a); ca != nil {
			res = append(res, ca)
		}
	}
	return res
}

func (c *GoToASTConverter) typeToString(e ast.Expr) string {
	return c.typeToStringWithDepth(e, 0)
}

func (c *GoToASTConverter) typeToStringWithDepth(e ast.Expr, depth int) string {
	if e == nil || depth > 10 {
		return "Any"
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
		switch name {
		case "int", "int8", "int16", "int32", "int64", "uint", "uint16", "uint32":
			return "Int64"
		case "float64", "float32":
			return "Float64"
		case "string":
			return "String"
		case "error":
			return "Error"
		case "bool":
			return "Bool"
		case "byte", "uint8":
			return "Uint8"
		case "any", "interface{}":
			return "Any"
		}
		// 检查是否是当前程序中定义的接口名
		if iface, ok := c.interfaces[name]; ok {
			return c.expandInterface(iface, depth+1)
		}
		return name
	case *ast.ArrayType:
		if ident, ok := t.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
			return "TypeBytes"
		}
		return "Array<" + c.typeToStringWithDepth(t.Elt, depth+1) + ">"
	case *ast.StarExpr:
		return "Ptr<" + c.typeToStringWithDepth(t.X, depth+1) + ">"
	case *ast.MapType:
		return "Map<" + c.typeToStringWithDepth(t.Key, depth+1) + ", " + c.typeToStringWithDepth(t.Value, depth+1) + ">"
	case *ast.SelectorExpr:
		return c.typeToStringWithDepth(t.X, depth+1) + "." + t.Sel.Name
	case *ast.Ellipsis:
		return "Array<" + c.typeToStringWithDepth(t.Elt, depth+1) + ">"
	case *ast.InterfaceType:
		return c.expandInterface(t, depth+1)
	}
	return "Any"
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
		return "Any"
	}
	return fmt.Sprintf("interface{%s}", strings.Join(methods, ";"))
}
