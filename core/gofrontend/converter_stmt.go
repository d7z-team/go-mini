package gofrontend

import (
	"fmt"
	"go/ast"
	"go/token"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func (c *Converter) convertStmt(s ast.Stmt) miniast.Stmt {
	if s == nil {
		return nil
	}
	switch st := s.(type) {
	case *ast.BadStmt:
		return c.badStmt(st, "无法解析的语句")
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
	case *ast.SendStmt:
		return &miniast.SendStmt{
			BaseNode: miniast.BaseNode{ID: c.genID(st, "send"), Meta: "send", Loc: c.extractLoc(st)},
			Channel:  c.convertExpr(st.Chan),
			Value:    c.convertExpr(st.Value),
		}
	case *ast.AssignStmt:
		rhsExprs := c.convertAssignRHS(st)
		if len(rhsExprs) == 0 {
			return c.badStmt(st, "赋值语句缺少右侧表达式")
		}
		if st.Tok == token.DEFINE {
			lhsExprs := make([]miniast.Expr, 0, len(st.Lhs))
			for _, lhs := range st.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "_" && len(st.Lhs) > 1 {
					lhsExprs = append(lhsExprs, nil)
					continue
				}
				lhsExprs = append(lhsExprs, c.convertExpr(lhs))
			}
			if len(lhsExprs) == 1 {
				return &miniast.AssignmentStmt{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
					Kind:     miniast.AssignDefine,
					LHS:      lhsExprs[0],
					Value:    rhsExprs[0],
				}
			}
			return &miniast.MultiAssignmentStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "multi_assignment"), Meta: "multi_assignment", Loc: c.extractLoc(st)},
				Kind:     miniast.AssignDefine,
				LHS:      lhsExprs,
				Values:   rhsExprs,
			}
		}
		if st.Tok == token.ASSIGN {
			if len(st.Lhs) == 1 {
				return &miniast.AssignmentStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)}, Kind: miniast.AssignSet, LHS: c.convertExpr(st.Lhs[0]), Value: rhsExprs[0]}
			}
			var lhsExprs []miniast.Expr
			for _, l := range st.Lhs {
				lhsExprs = append(lhsExprs, c.convertExpr(l))
			}
			return &miniast.MultiAssignmentStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "multi_assignment"), Meta: "multi_assignment", Loc: c.extractLoc(st)}, Kind: miniast.AssignSet, LHS: lhsExprs, Values: rhsExprs}
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
			return c.badStmt(st, "不支持的复合赋值操作: "+st.Tok.String())
		}
		if len(st.Lhs) == 1 && len(st.Rhs) == 1 {
			lhs := c.convertExpr(st.Lhs[0])
			return &miniast.AssignmentStmt{
				BaseNode: miniast.BaseNode{ID: c.genID(st, "assignment"), Meta: "assignment", Loc: c.extractLoc(st)},
				Kind:     miniast.AssignSet,
				LHS:      lhs,
				Value: &miniast.BinaryExpr{
					BaseNode: miniast.BaseNode{ID: c.genID(st, "binary"), Meta: "binary", Loc: c.extractLoc(st)},
					Left:     lhs, Operator: miniast.Ident(c.convertOp(op)), Right: c.convertExpr(st.Rhs[0]),
				},
			}
		}
		return c.badStmt(st, "不支持的复合赋值形式")
	case *ast.DeclStmt:
		if decl, ok := st.Decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
			var children []miniast.Stmt
			for _, spec := range decl.Specs {
				if vSpec, ok := spec.(*ast.ValueSpec); ok {
					children = append(children, c.convertValueSpecDecl(vSpec))
				}
			}
			return &miniast.BlockStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "block"), Meta: "block", Loc: c.extractLoc(st)}, Inner: true, Children: children}
		}
		return c.badStmt(st, "只支持 var 声明语句")
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
			key, ok := st.Key.(*ast.Ident)
			if !ok {
				return c.badStmt(st.Key, "range 的 key 目标只支持标识符")
			}
			res.Key = miniast.Ident(key.Name)
		}
		if st.Value != nil {
			value, ok := st.Value.(*ast.Ident)
			if !ok {
				return c.badStmt(st.Value, "range 的 value 目标只支持标识符")
			}
			res.Value = miniast.Ident(value.Name)
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
					if stmt := c.convertStmt(bStmt); stmt != nil {
						cClause.Body = append(cClause.Body, stmt)
					}
				}
				res.Body.Children = append(res.Body.Children, cClause)
			}
		}
		if st.Init != nil {
			return res
		}
		return res
	case *ast.SelectStmt:
		res := &miniast.SelectStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "select"), Meta: "select", Loc: c.extractLoc(st)}}
		if st.Body != nil {
			for _, stmt := range st.Body.List {
				clause, ok := stmt.(*ast.CommClause)
				if !ok {
					continue
				}
				cCase := miniast.SelectCase{BaseNode: miniast.BaseNode{ID: c.genID(clause, "select_case"), Meta: "select_case", Loc: c.extractLoc(clause)}}
				if clause.Comm != nil {
					cCase.Comm = c.convertStmt(clause.Comm)
				}
				for _, bodyStmt := range clause.Body {
					if converted := c.convertStmt(bodyStmt); converted != nil {
						cCase.Body = append(cCase.Body, converted)
					}
				}
				res.Cases = append(res.Cases, cCase)
			}
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
					if stmt := c.convertStmt(bStmt); stmt != nil {
						cClause.Body = append(cClause.Body, stmt)
					}
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
		return c.badStmt(st, "defer 只支持函数调用")
	case *ast.GoStmt:
		call := c.convertExpr(st.Call)
		if cExpr, ok := call.(*miniast.CallExprStmt); ok {
			return &miniast.GoStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "go"), Meta: "go", Loc: c.extractLoc(st)}, Call: cExpr}
		}
		return c.badStmt(st, "go 只支持函数调用")
	case *ast.BlockStmt:
		return c.toBlock(st)
	case *ast.IncDecStmt:
		return &miniast.IncDecStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "increment"), Meta: "increment", Loc: c.extractLoc(st)}, Operand: c.convertExpr(st.X), Operator: miniast.Ident(st.Tok.String())}
	case *ast.BranchStmt:
		if st.Tok == token.BREAK || st.Tok == token.CONTINUE {
			return &miniast.InterruptStmt{BaseNode: miniast.BaseNode{ID: c.genID(st, "interrupt"), Meta: "interrupt", Loc: c.extractLoc(st)}, InterruptType: st.Tok.String()}
		}
		return c.badStmt(st, "不支持的跳转语句: "+st.Tok.String())
	}
	return c.badStmt(s, fmt.Sprintf("不支持的语句: %T", s))
}

func (c *Converter) toBlock(s ast.Stmt) *miniast.BlockStmt {
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
