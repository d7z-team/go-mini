package ast

// AssignNodeIDs replaces node and source-name IDs with a deterministic source-order walk.
func AssignNodeIDs(program *Program) {
	if program == nil {
		return
	}
	nextNode, nextName := NodeID(1), NameID(1)
	assignNode := func(id *NodeID) { *id, nextNode = nextNode, nextNode+1 }
	assignName := func(name *Identifier) {
		if name.Text != "" {
			name.ID, nextName = nextName, nextName+1
		}
	}
	assignNode(&program.NodeID)
	assignName(&program.PackageID)
	for i := range program.Files {
		file := &program.Files[i]
		assignNode(&file.NodeID)
		assignName(&file.PackageID)
		for j := range file.Decls {
			assignDeclIDs(&file.Decls[j], assignNode, assignName)
		}
	}
}

func assignDeclIDs(decl *Decl, node func(*NodeID), name func(*Identifier)) {
	node(&decl.NodeID)
	name(&decl.Import.AliasID)
	switch decl.Kind {
	case DeclConst:
		assignValueDeclIDs(&decl.Const, node, name)
	case DeclVar:
		assignValueDeclIDs(&decl.Var, node, name)
	case DeclType:
		name(&decl.Type.NameID)
		for i := range decl.Type.TypeParams {
			assignTypeParamIDs(&decl.Type.TypeParams[i], node, name)
		}
		assignTypeIDs(&decl.Type.Type, node, name)
	case DeclFunc:
		assignFuncIDs(&decl.Func, node, name)
	}
}

func assignValueDeclIDs(decl *ValueDecl, node func(*NodeID), name func(*Identifier)) {
	for i := range decl.NameIDs {
		name(&decl.NameIDs[i])
	}
	assignTypeIDs(&decl.Type, node, name)
	for i := range decl.Values {
		assignExpressionIDs(&decl.Values[i], node, name)
	}
}

func assignFuncIDs(decl *FuncDecl, node func(*NodeID), name func(*Identifier)) {
	node(&decl.NodeID)
	name(&decl.NameID)
	if decl.Receiver != nil {
		assignFieldIDs(decl.Receiver, node, name)
	}
	for i := range decl.TypeParams {
		assignTypeParamIDs(&decl.TypeParams[i], node, name)
	}
	for i := range decl.Params {
		assignFieldIDs(&decl.Params[i], node, name)
	}
	for i := range decl.Results {
		assignFieldIDs(&decl.Results[i], node, name)
	}
	assignBlockIDs(&decl.Body, node, name)
}

func assignTypeParamIDs(param *TypeParam, node func(*NodeID), name func(*Identifier)) {
	node(&param.NodeID)
	name(&param.NameID)
	assignTypeIDs(&param.Constraint, node, name)
}

func assignFieldIDs(field *Field, node func(*NodeID), name func(*Identifier)) {
	node(&field.NodeID)
	name(&field.NameID)
	assignTypeIDs(&field.Type, node, name)
}

func assignTypeIDs(typ *TypeExpr, node func(*NodeID), name func(*Identifier)) {
	if typ == nil || typ.Kind == TypeInvalid {
		return
	}
	node(&typ.NodeID)
	name(&typ.NameID)
	name(&typ.QualifierID)
	assignTypeIDs(typ.Base, node, name)
	for i := range typ.TypeArgs {
		assignTypeIDs(&typ.TypeArgs[i], node, name)
	}
	assignTypeIDs(typ.Elem, node, name)
	assignTypeIDs(typ.Key, node, name)
	if typ.Len != nil {
		assignExpressionIDs(typ.Len, node, name)
	}
	for i := range typ.Params {
		assignFieldIDs(&typ.Params[i], node, name)
	}
	for i := range typ.Results {
		assignFieldIDs(&typ.Results[i], node, name)
	}
	for i := range typ.Fields {
		assignFieldIDs(&typ.Fields[i], node, name)
	}
	for i := range typ.Methods {
		assignFuncIDs(&typ.Methods[i], node, name)
	}
	for i := range typ.Embeds {
		assignTypeIDs(&typ.Embeds[i], node, name)
	}
	for i := range typ.Terms {
		node(&typ.Terms[i].NodeID)
		assignTypeIDs(&typ.Terms[i].Type, node, name)
	}
}

func assignBlockIDs(block *BlockStmt, node func(*NodeID), name func(*Identifier)) {
	if block == nil || (!block.Span.Valid() && len(block.Stmts) == 0) {
		return
	}
	node(&block.NodeID)
	for i := range block.Stmts {
		assignStatementIDs(&block.Stmts[i], node, name)
	}
}

func assignStatementIDs(stmt *Statement, node func(*NodeID), name func(*Identifier)) {
	if stmt == nil {
		return
	}
	node(&stmt.NodeID)
	name(&stmt.LabelID)
	name(&stmt.TypeSwitchID)
	for i := range stmt.Decls {
		assignDeclIDs(&stmt.Decls[i], node, name)
	}
	if stmt.Expr != nil {
		assignExpressionIDs(stmt.Expr, node, name)
	}
	for i := range stmt.Left {
		assignExpressionIDs(&stmt.Left[i], node, name)
	}
	for i := range stmt.Right {
		assignExpressionIDs(&stmt.Right[i], node, name)
	}
	assignBlockIDs(&stmt.Body, node, name)
	assignStatementIDs(stmt.Init, node, name)
	if stmt.Cond != nil {
		assignExpressionIDs(stmt.Cond, node, name)
	}
	assignStatementIDs(stmt.Post, node, name)
	assignStatementIDs(stmt.Else, node, name)
	if stmt.Key != nil {
		assignExpressionIDs(stmt.Key, node, name)
	}
	if stmt.Value != nil {
		assignExpressionIDs(stmt.Value, node, name)
	}
	if stmt.Range != nil {
		assignExpressionIDs(stmt.Range, node, name)
	}
	for i := range stmt.Cases {
		clause := &stmt.Cases[i]
		node(&clause.NodeID)
		for j := range clause.Values {
			assignExpressionIDs(&clause.Values[j], node, name)
		}
		for j := range clause.Types {
			assignTypeIDs(&clause.Types[j], node, name)
		}
		assignStatementIDs(clause.Comm, node, name)
		assignBlockIDs(&clause.Body, node, name)
	}
	for i := range stmt.Results {
		assignExpressionIDs(&stmt.Results[i], node, name)
	}
}

func assignExpressionIDs(expr *Expression, node func(*NodeID), name func(*Identifier)) {
	if expr == nil || expr.Kind == ExprInvalid {
		return
	}
	node(&expr.NodeID)
	name(&expr.NameID)
	name(&expr.FieldID)
	assignTypeIDs(&expr.Type, node, name)
	assignExpressionIDs(expr.Left, node, name)
	assignExpressionIDs(expr.Right, node, name)
	assignExpressionIDs(expr.Operand, node, name)
	assignExpressionIDs(expr.Callee, node, name)
	for i := range expr.Args {
		assignExpressionIDs(&expr.Args[i], node, name)
	}
	assignExpressionIDs(expr.Index, node, name)
	assignExpressionIDs(expr.Start, node, name)
	assignExpressionIDs(expr.End, node, name)
	assignExpressionIDs(expr.Max, node, name)
	for i := range expr.Elements {
		assignExpressionIDs(&expr.Elements[i], node, name)
	}
	for _, entries := range [][]KeyValue{expr.Entries, expr.Items} {
		for i := range entries {
			node(&entries[i].NodeID)
			assignExpressionIDs(entries[i].Key, node, name)
			assignExpressionIDs(&entries[i].Value, node, name)
		}
	}
	if expr.Kind == ExprFunc {
		assignFuncIDs(&expr.Func, node, name)
	}
}
