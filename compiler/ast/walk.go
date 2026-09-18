package ast

import "github.com/d7z-team/mini-go/compiler/source"

// WalkExpressions visits every source expression once in lexical AST order.
func WalkExpressions(program *Program, visit func(*Expression)) {
	walkNodes(program, visit, nil, nil)
}

// WalkTypes visits every source type expression once in lexical AST order.
func WalkTypes(program *Program, visit func(*TypeExpr)) {
	walkNodes(program, nil, visit, nil)
}

// WalkSpans visits the source ranges of declarations, blocks, statements,
// expressions and types in lexical AST order.
func WalkSpans(program *Program, visit func(source.Span)) {
	walkNodes(program, nil, nil, visit)
}

func walkNodes(program *Program, visitExpr func(*Expression), visitType func(*TypeExpr), visitSpan func(source.Span)) {
	if program == nil || visitExpr == nil && visitType == nil && visitSpan == nil {
		return
	}
	var walkExpr func(*Expression)
	var walkType func(*TypeExpr)
	var walkDecl func(*Decl)
	var walkStmt func(*Statement)
	var walkBlock func(*BlockStmt)
	var walkFunc func(*FuncDecl)
	walkExpr = func(expr *Expression) {
		if expr == nil {
			return
		}
		if visitExpr != nil {
			visitExpr(expr)
		}
		if visitSpan != nil {
			visitSpan(expr.Span)
		}
		walkType(&expr.Type)
		walkExpr(expr.Left)
		walkExpr(expr.Right)
		walkExpr(expr.Operand)
		walkExpr(expr.Callee)
		for i := range expr.Args {
			walkExpr(&expr.Args[i])
		}
		walkExpr(expr.Index)
		walkExpr(expr.Start)
		walkExpr(expr.End)
		walkExpr(expr.Max)
		if len(expr.Items) != 0 {
			for i := range expr.Items {
				walkExpr(expr.Items[i].Key)
				walkExpr(&expr.Items[i].Value)
			}
		} else {
			for i := range expr.Elements {
				walkExpr(&expr.Elements[i])
			}
			for i := range expr.Entries {
				walkExpr(expr.Entries[i].Key)
				walkExpr(&expr.Entries[i].Value)
			}
		}
		if expr.Kind == ExprFunc {
			walkFunc(&expr.Func)
		}
	}
	walkType = func(typ *TypeExpr) {
		if typ == nil {
			return
		}
		if visitType != nil {
			visitType(typ)
		}
		if visitSpan != nil {
			visitSpan(typ.Span)
		}
		walkType(typ.Base)
		walkType(typ.Elem)
		walkType(typ.Key)
		walkExpr(typ.Len)
		for _, fields := range [][]Field{typ.Params, typ.Results, typ.Fields} {
			for i := range fields {
				walkType(&fields[i].Type)
			}
		}
		for i := range typ.Methods {
			walkFunc(&typ.Methods[i])
		}
		for i := range typ.Embeds {
			walkType(&typ.Embeds[i])
		}
		for i := range typ.Terms {
			walkType(&typ.Terms[i].Type)
		}
		for i := range typ.TypeArgs {
			walkType(&typ.TypeArgs[i])
		}
	}
	walkBlock = func(block *BlockStmt) {
		if block == nil {
			return
		}
		if visitSpan != nil {
			visitSpan(block.Span)
		}
		for i := range block.Stmts {
			walkStmt(&block.Stmts[i])
		}
	}
	walkFunc = func(function *FuncDecl) {
		if function == nil {
			return
		}
		if function.Receiver != nil {
			walkType(&function.Receiver.Type)
		}
		for i := range function.TypeParams {
			walkType(&function.TypeParams[i].Constraint)
		}
		for _, fields := range [][]Field{function.Params, function.Results} {
			for i := range fields {
				walkType(&fields[i].Type)
			}
		}
		walkBlock(&function.Body)
	}
	walkDecl = func(decl *Decl) {
		if decl == nil {
			return
		}
		if visitSpan != nil {
			visitSpan(decl.Span)
		}
		switch decl.Kind {
		case DeclConst:
			walkType(&decl.Const.Type)
			for i := range decl.Const.Values {
				walkExpr(&decl.Const.Values[i])
			}
		case DeclVar:
			walkType(&decl.Var.Type)
			for i := range decl.Var.Values {
				walkExpr(&decl.Var.Values[i])
			}
		case DeclType:
			for i := range decl.Type.TypeParams {
				walkType(&decl.Type.TypeParams[i].Constraint)
			}
			walkType(&decl.Type.Type)
		case DeclFunc:
			walkFunc(&decl.Func)
		}
	}
	walkStmt = func(stmt *Statement) {
		if stmt == nil {
			return
		}
		if visitSpan != nil {
			visitSpan(stmt.Span)
		}
		for i := range stmt.Decls {
			walkDecl(&stmt.Decls[i])
		}
		walkExpr(stmt.Expr)
		for _, expressions := range [][]Expression{stmt.Left, stmt.Right, stmt.Results} {
			for i := range expressions {
				walkExpr(&expressions[i])
			}
		}
		walkBlock(&stmt.Body)
		walkStmt(stmt.Init)
		walkExpr(stmt.Cond)
		walkStmt(stmt.Post)
		walkStmt(stmt.Else)
		walkExpr(stmt.Key)
		walkExpr(stmt.Value)
		walkExpr(stmt.Range)
		for i := range stmt.Cases {
			for j := range stmt.Cases[i].Values {
				walkExpr(&stmt.Cases[i].Values[j])
			}
			for j := range stmt.Cases[i].Types {
				walkType(&stmt.Cases[i].Types[j])
			}
			walkStmt(stmt.Cases[i].Comm)
			walkBlock(&stmt.Cases[i].Body)
		}
	}
	for i := range program.Files {
		for j := range program.Files[i].Decls {
			walkDecl(&program.Files[i].Decls[j])
		}
	}
}
