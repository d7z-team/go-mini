package ffigo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"hash/fnv"
	"strconv"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

type GoToASTConverter struct {
	fset    *token.FileSet
	imports map[string]string // Alias -> Path
}

func (c *GoToASTConverter) genID(node ast.Node, meta string) string {
	if node == nil {
		return "meta_" + meta
	}
	pos := c.fset.Position(node.Pos())
	h := fnv.New64a()
	// 组合 文件名:行:列:类型 确保 ID 在代码未改动时绝对稳定
	fmt.Fprintf(h, "%s:%d:%d:%s", pos.Filename, pos.Line, pos.Column, meta)
	return strconv.FormatUint(h.Sum64(), 16)
}

func (c *GoToASTConverter) extractLoc(node ast.Node) *miniast.Position {
	if node == nil || c.fset == nil {
		return nil
	}
	pos := c.fset.Position(node.Pos())
	if pos.Line == 0 {
		return nil
	}
	return &miniast.Position{
		F: pos.Filename,
		L: pos.Line,
		C: pos.Column,
	}
}

func NewGoToASTConverter() *GoToASTConverter {
	return &GoToASTConverter{
		fset:    token.NewFileSet(),
		imports: make(map[string]string),
	}
}

func (c *GoToASTConverter) ConvertSource(code string) (miniast.Node, error) {
	f, err := parser.ParseFile(c.fset, "", code, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// 记录导入
	c.imports = make(map[string]string)
	var miniImports []miniast.ImportSpec
	for _, imp := range f.Imports {
		path := imp.Path.Value[1 : len(imp.Path.Value)-1]
		var alias string
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			// 简单处理：使用路径最后一段作为包名
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}
		c.imports[alias] = path
		miniImports = append(miniImports, miniast.ImportSpec{
			Alias: alias,
			Path:  path,
		})
	}

	program := &miniast.ProgramStmt{
		BaseNode:  miniast.BaseNode{ID: c.genID(f, "boot"), Meta: "boot", Type: "Void", Loc: c.extractLoc(f)},
		Package:   f.Name.Name,
		Constants: make(map[string]string),
		Variables: make(map[miniast.Ident]miniast.Expr),
		Structs:   make(map[miniast.Ident]*miniast.StructStmt),
		Functions: make(map[miniast.Ident]*miniast.FunctionStmt),
		Imports:   miniImports,
	}

	// 将导入转换为变量声明 (动态模块对象)
	for i, imp := range miniImports {
		program.Variables[miniast.Ident(imp.Alias)] = &miniast.ImportExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(f.Imports[i], "import"), Meta: "import", Type: miniast.TypeModule},
			Path:     imp.Path,
		}
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
						program.Structs[miniast.Ident(s.Name.Name)] = c.convertStruct(s.Name.Name, st)
					}
				case *ast.ValueSpec:
					switch d.Tok {
					case token.CONST:
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
					case token.VAR:
						for i, name := range s.Names {
							var val miniast.Expr
							if i < len(s.Values) {
								val = c.convertExpr(s.Values[i])
							}
							program.Variables[miniast.Ident(name.Name)] = val
						}
					}
				}
			}
		}
	}

	return program, nil
}

func (c *GoToASTConverter) ConvertExprSource(code string) (miniast.Expr, error) {
	e, err := parser.ParseExpr(code)
	if err != nil {
		return nil, err
	}
	return c.convertExpr(e), nil
}

func (c *GoToASTConverter) ConvertStmtsSource(code string) ([]miniast.Stmt, error) {
	// 包装为完整的函数以便解析
	wrapper := fmt.Sprintf("package main\nfunc main() {\n%s\n}", code)
	node, err := c.ConvertSource(wrapper)
	if err != nil {
		return nil, err
	}
	prog := node.(*miniast.ProgramStmt)
	// 如果转换器已经把 main 提取到了 Main，则直接返回
	if len(prog.Main) > 0 {
		return prog.Main, nil
	}
	// 否则从 Functions 中寻找 main 函数并提取其主体
	if mainFunc, ok := prog.Functions["main"]; ok && mainFunc.Body != nil {
		return mainFunc.Body.Children, nil
	}
	return nil, nil
}

func (c *GoToASTConverter) convertStruct(name string, s *ast.StructType) *miniast.StructStmt {
	res := &miniast.StructStmt{
		BaseNode: miniast.BaseNode{ID: c.genID(s, "struct"), Meta: "struct", Loc: c.extractLoc(s)},
		Name:     miniast.Ident(name),
		Fields:   make(map[miniast.Ident]miniast.GoMiniType),
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
			params = append(params, miniast.FunctionParam{
				Name: miniast.Ident(recv.Names[0].Name),
				Type: miniast.GoMiniType(typeName),
			})
		} else {
			params = append(params, miniast.FunctionParam{
				Name: "_",
				Type: miniast.GoMiniType(typeName),
			})
		}
	}

	fn := &miniast.FunctionStmt{
		BaseNode: miniast.BaseNode{ID: c.genID(d, "function"), Meta: "function", Loc: c.extractLoc(d)},
		Name:     miniast.Ident(fnName),
		Body:     &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(d.Body, "block"), Meta: "block", Loc: c.extractLoc(d.Body)}},
		FunctionType: miniast.FunctionType{
			Params: params,
		},
	}
	// Params
	if d.Type.Params != nil {
		for _, p := range d.Type.Params.List {
			t := c.typeToString(p.Type)
			if _, isVariadic := p.Type.(*ast.Ellipsis); isVariadic {
				fn.Variadic = true
			}
			for _, name := range p.Names {
				fn.Params = append(fn.Params, miniast.FunctionParam{
					Name: miniast.Ident(name.Name),
					Type: miniast.GoMiniType(t),
				})
			}
		}
	}
	// Return
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		fn.Return = miniast.GoMiniType(c.typeToString(d.Type.Results.List[0].Type))
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

func (c *GoToASTConverter) convertStmt(s ast.Stmt) miniast.Stmt {
	if s == nil {
		return nil
	}
	switch st := s.(type) {
	case *ast.ExprStmt:
		expr := c.convertExpr(st.X)
		if call, ok := expr.(*miniast.CallExprStmt); ok {
			return call
		}
		return nil
	case *ast.ReturnStmt:
		res := &miniast.ReturnStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "return"), Meta: "return", Loc: c.extractLoc(st)}}
		for _, r := range st.Results {
			res.Results = append(res.Results, c.convertExpr(r))
		}
		return res
	case *ast.AssignStmt:
		if len(st.Rhs) != 1 {
			// 目前仅支持 a, b = f() 这种单右值解构，不支持 a, b = 1, 2
			return nil
		}
		rhs := st.Rhs[0]

		if st.Tok == token.DEFINE { // :=
			var children []miniast.Stmt
			var lhsExprs []miniast.Expr

			for _, lhs := range st.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok {
					// 1. Declare variable
					children = append(children, &miniast.GenDeclStmt{
						BaseNode: miniast.BaseNode{ID: c.genID(lhs, "decl"), Meta: "decl", Loc: c.extractLoc(lhs)},
						Name:     miniast.Ident(ident.Name),
						Kind:     "Any",
					})
					// 2. Prepare LHS list
					lhsExprs = append(lhsExprs, &miniast.IdentifierExpr{
						BaseNode: miniast.BaseNode{ID: c.genID(lhs, "identifier"), Meta: "identifier"},
						Name:     miniast.Ident(ident.Name),
					})
				}
			}

			if len(lhsExprs) == 1 {
				children = append(children, &miniast.AssignmentStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
					LHS:      lhsExprs[0],
					Value:    c.convertExpr(rhs),
				})
			} else {
				children = append(children, &miniast.MultiAssignmentStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "multi_assignment"), Meta: "multi_assignment", Loc: c.extractLoc(st)},
					LHS:      lhsExprs,
					Value:    c.convertExpr(rhs),
				})
			}

			return &miniast.BlockStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)},
				Inner:    true,
				Children: children,
			}
		}

		if st.Tok == token.ASSIGN {
			if len(st.Lhs) == 1 {
				return &miniast.AssignmentStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
					LHS:      c.convertExpr(st.Lhs[0]),
					Value:    c.convertExpr(rhs),
				}
			}
			var lhsExprs []miniast.Expr
			for _, l := range st.Lhs {
				lhsExprs = append(lhsExprs, c.convertExpr(l))
			}
			return &miniast.MultiAssignmentStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "multi_assignment"), Meta: "multi_assignment", Loc: c.extractLoc(st)},
				LHS:      lhsExprs,
				Value:    c.convertExpr(rhs),
			}
		}

		// 处理复合赋值: a += b => a = a + b
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

		if len(st.Lhs) == 1 {
			lhs := c.convertExpr(st.Lhs[0])
			return &miniast.AssignmentStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
				LHS:      lhs,
				Value: &miniast.BinaryExpr{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "binary"), Meta: "binary"},
					Left:     lhs,
					Operator: miniast.Ident(c.convertOp(op)),
					Right:    c.convertExpr(rhs),
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
							Name:     miniast.Ident(name.Name),
							Kind:     miniast.GoMiniType(vType),
						})
						if i < len(vSpec.Values) {
							children = append(children, &miniast.AssignmentStmt{
								BaseNode: miniast.BaseNode{ID: c.genID(name, "assignment"), Meta: "assignment", Loc: c.extractLoc(name)},
								LHS:      &miniast.IdentifierExpr{BaseNode: miniast.BaseNode{ID: c.genID(name, "identifier"), Meta: "identifier"}, Name: miniast.Ident(name.Name)},
								Value:    c.convertExpr(vSpec.Values[i]),
							})
						}
					}
				}
			}
			return &miniast.BlockStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)},
				Inner:    true,
				Children: children,
			}
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
			return &miniast.BlockStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)},
				Inner:    true,
				Children: []miniast.Stmt{
					c.convertStmt(st.Init),
					res,
				},
			}
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
		return res

	case *ast.DeferStmt:
		call := c.convertExpr(st.Call)
		if cExpr, ok := call.(*miniast.CallExprStmt); ok {
			return &miniast.DeferStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "defer"), Meta: "defer", Loc: c.extractLoc(st)},
				Call:     cExpr,
			}
		}
		return nil

	case *ast.BlockStmt:
		return c.toBlock(st)

	case *ast.IncDecStmt:
		return &miniast.IncDecStmt{
			BaseNode: miniast.BaseNode{ID: c.genID(st, "increment"), Meta: "increment", Loc: c.extractLoc(st)},
			Operand:  c.convertExpr(st.X),
			Operator: miniast.Ident(st.Tok.String()),
		}

	case *ast.BranchStmt:
		if st.Tok == token.BREAK || st.Tok == token.CONTINUE {
			return &miniast.InterruptStmt{
				BaseNode:      miniast.BaseNode{ID: c.genID(st, "interrupt"), Meta: "interrupt", Loc: c.extractLoc(st)},
				InterruptType: st.Tok.String(),
			}
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
	// Wrap single statement
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
		return &miniast.LiteralExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "literal"), Meta: "literal", Type: miniast.GoMiniType(t)}, Value: val}
	case *ast.Ident:
		if ex.Name == "true" || ex.Name == "false" {
			return &miniast.LiteralExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "literal"), Meta: "literal", Type: "Bool"}, Value: ex.Name}
		}
		// 特殊处理内建函数，它们应该是 ConstRefExpr 以便在验证阶段找到签名
		switch ex.Name {
		case "panic", "make", "append", "delete", "len", "require":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref"}, Name: miniast.Ident(ex.Name)}
		case "int", "int64":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref"}, Name: "Int64"}
		case "float64":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref"}, Name: "Float64"}
		case "string":
			return &miniast.ConstRefExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "const_ref"), Meta: "const_ref"}, Name: "String"}
		}
		return &miniast.IdentifierExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "identifier"), Meta: "identifier"}, Name: miniast.Ident(ex.Name)}
	case *ast.BinaryExpr:
		return &miniast.BinaryExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "binary"), Meta: "binary"}, Left: c.convertExpr(ex.X), Operator: miniast.Ident(c.convertOp(ex.Op)), Right: c.convertExpr(ex.Y)}
	case *ast.UnaryExpr:
		return &miniast.UnaryExpr{BaseNode: miniast.BaseNode{ID: c.genID(ex, "unary"), Meta: "unary"}, Operator: miniast.Ident(c.convertOp(ex.Op)), Operand: c.convertExpr(ex.X)}
	case *ast.ParenExpr:
		return c.convertExpr(ex.X)
	case *ast.CallExpr:
		funExpr := c.convertExpr(ex.Fun)
		// 如果被调者是一个标识符，将其转换为 ConstRefExpr 以匹配 Validator 预期
		if ident, ok := funExpr.(*miniast.IdentifierExpr); ok {
			funExpr = &miniast.ConstRefExpr{
				BaseNode: miniast.BaseNode{ID: c.genID(ex.Fun, "const_ref"), Meta: "const_ref"},
				Name:     ident.Name,
			}
		}
		// 特殊处理类型转换
		if array, ok := ex.Fun.(*ast.ArrayType); ok {
			if ident, ok := array.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
				funExpr = &miniast.ConstRefExpr{
					BaseNode: miniast.BaseNode{ID: c.genID(ex.Fun, "const_ref"), Meta: "const_ref"},
					Name:     "TypeBytes",
				}
			}
		}

		// 特殊处理内建函数
		if ident, ok := funExpr.(*miniast.ConstRefExpr); ok {
			switch ident.Name {
			case "make", "new":
				if len(ex.Args) > 0 {
					typeArg := c.typeToString(ex.Args[0])
					args := []miniast.Expr{
						&miniast.LiteralExpr{
							BaseNode: miniast.BaseNode{ID: c.genID(ex.Args[0], "literal"), Meta: "literal", Type: "String"},
							Value:    typeArg,
						},
					}
					args = append(args, c.convertArgs(ex.Args[1:])...)
					return &miniast.CallExprStmt{
						BaseNode: miniast.BaseNode{ID: c.genID(ex, "call"), Meta: "call", Loc: c.extractLoc(ex)},
						Func:     funExpr,
						Args:     args,
					}
				}
			case "require":
				if len(ex.Args) == 1 {
					if lit, ok := ex.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						path := lit.Value[1 : len(lit.Value)-1]
						return &miniast.ImportExpr{
							BaseNode: miniast.BaseNode{ID: c.genID(ex, "import"), Meta: "import", Type: miniast.TypeModule},
							Path:     path,
						}
					}
				}
			}
		}

		return &miniast.CallExprStmt{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "call"), Meta: "call", Loc: c.extractLoc(ex)},
			Func:     funExpr,
			Args:     c.convertArgs(ex.Args),
		}
	case *ast.CompositeLit:
		typeName := c.typeToString(ex.Type)
		res := &miniast.CompositeExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "composite"), Meta: "composite", Loc: c.extractLoc(ex)},
			Kind:     miniast.Ident(typeName),
			Values:   make([]miniast.CompositeElement, len(ex.Elts)),
		}
		for i, elt := range ex.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				var keyExpr miniast.Expr
				if ident, ok := kv.Key.(*ast.Ident); ok {
					// 结构体字段名不应作为 IdentifierExpr 被 Check
					// 保持为 nil 或特殊的字面量，由 CompositeExpr.Check 处理
					keyExpr = &miniast.IdentifierExpr{
						BaseNode: miniast.BaseNode{ID: c.genID(ident, "identifier"), Meta: "identifier"},
						Name:     miniast.Ident(ident.Name),
					}
				} else {
					keyExpr = c.convertExpr(kv.Key)
				}
				res.Values[i] = miniast.CompositeElement{
					Key:   keyExpr,
					Value: c.convertExpr(kv.Value),
				}
			} else {
				res.Values[i] = miniast.CompositeElement{
					Value: c.convertExpr(elt),
				}
			}
		}
		return res
	case *ast.SelectorExpr:
		// 统一视为对象成员访问（动态绑定）
		// 包成员现在通过 ImportExpr 映射为局部变量，因此这里 X.Sel 会自动解析
		return &miniast.MemberExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "member"), Meta: "member"},
			Object:   c.convertExpr(ex.X),
			Property: miniast.Ident(ex.Sel.Name),
		}
	case *ast.IndexExpr:
		return &miniast.IndexExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "index"), Meta: "index"},
			Object:   c.convertExpr(ex.X),
			Index:    c.convertExpr(ex.Index),
		}
	case *ast.SliceExpr:
		res := &miniast.SliceExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "slice"), Meta: "slice"},
			X:        c.convertExpr(ex.X),
		}
		if ex.Low != nil {
			res.Low = c.convertExpr(ex.Low)
		}
		if ex.High != nil {
			res.High = c.convertExpr(ex.High)
		}
		return res
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
		if ex.Type.Results != nil && len(ex.Type.Results.List) > 0 {
			retType = c.typeToString(ex.Type.Results.List[0].Type)
		}

		funcExpr := &miniast.FuncLitExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(ex, "func_lit"), Meta: "func_lit", Loc: c.extractLoc(ex)},
			FunctionType: miniast.FunctionType{
				Params: params,
				Return: miniast.GoMiniType(retType),
			},
		}
		if ex.Body != nil {
			funcExpr.Body = c.convertStmt(ex.Body).(*miniast.BlockStmt)
			funcExpr.Body.Inner = true
		}
		// Capture analysis will be performed during semantic validation (ast_valid.go)
		// because it's much more accurate to resolve local vs external variables there.
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
	if e == nil {
		return ""
	}
	switch t := e.(type) {
	case *ast.BasicLit:
		val := t.Value
		if t.Kind == token.STRING && len(val) >= 2 {
			val = val[1 : len(val)-1]
		}
		return val
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
		if name == "byte" || name == "uint8" {
			return "Uint8"
		}
		if name == "any" || name == "interface{}" {
			return "Any"
		}
		return name
	case *ast.ArrayType:
		if ident, ok := t.Elt.(*ast.Ident); ok && (ident.Name == "byte" || ident.Name == "uint8") {
			return "TypeBytes"
		}
		return fmt.Sprintf("Array<%s>", c.typeToString(t.Elt))
	case *ast.StarExpr:
		return fmt.Sprintf("Ptr<%s>", c.typeToString(t.X))
	case *ast.MapType:
		return fmt.Sprintf("Map<%s, %s>", c.typeToString(t.Key), c.typeToString(t.Value))
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", c.typeToString(t.X), t.Sel.Name)
	case *ast.Ellipsis:
		return fmt.Sprintf("Array<%s>", c.typeToString(t.Elt))
	}
	return "Any"
}
