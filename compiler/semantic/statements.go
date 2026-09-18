package semantic

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) analyzeBlock(block *ast.BlockStmt, parent ScopeID, child bool) {
	if block == nil || block.NodeID == 0 {
		return
	}
	scope := parent
	if child {
		scope = a.newScope(ScopeBlock, block.NodeID, parent)
	} else {
		a.info.NodeScopes[block.NodeID] = scope
	}
	for i := range block.Stmts {
		a.analyzeStmt(&block.Stmts[i], scope)
	}
}

func (a *analyzer) analyzeStmt(stmt *ast.Statement, scope ScopeID) {
	if stmt.Kind == ast.StmtRange {
		a.analyzeRangeStmt(stmt, scope)
		return
	}
	if stmt.Init != nil {
		scope = a.newScope(ScopeBlock, stmt.NodeID, scope)
		a.analyzeStmt(stmt.Init, scope)
	}
	a.info.NodeScopes[stmt.NodeID] = scope
	for i := range stmt.Decls {
		a.analyzeDecl(&stmt.Decls[i], scope, true)
	}
	if stmt.Expr != nil {
		a.analyzeExpr(stmt.Expr, scope)
		if stmt.Kind == ast.StmtExpr {
			a.validateExpressionStatement(*stmt.Expr)
		}
	}
	for i := range stmt.Left {
		if stmt.Kind == ast.StmtAssign && stmt.Op == ":=" && stmt.Left[i].Kind == ast.ExprIdent {
			continue
		}
		a.analyzeExpr(&stmt.Left[i], scope)
	}
	for i := range stmt.Right {
		a.analyzeExpr(&stmt.Right[i], scope)
		if stmt.Kind == ast.StmtAssign && a.info.Exprs[stmt.Right[i].NodeID].Mode == ExprNoValue {
			a.addDiagnostic("semantic.assignment.no_value", "assignment expression produces no value", stmt.Right[i].Span)
		}
	}
	if stmt.Kind == ast.StmtAssign && stmt.Op != ":=" {
		for _, target := range stmt.Left {
			info, ok := a.info.Exprs[target.NodeID]
			if ok && info.Type.Valid() && !info.Category.Assignable() {
				a.addDiagnostic("semantic.assign.target", "assignment target is not assignable", target.Span)
			}
		}
	}
	if operator, ok := compoundAssignmentOperator(stmt.Op); ok && len(stmt.Left) == 1 && len(stmt.Right) == 1 {
		left := a.info.Exprs[stmt.Left[0].NodeID]
		right := a.info.Exprs[stmt.Right[0].NodeID]
		expression := ast.Expression{NodeID: stmt.NodeID, Kind: ast.ExprBinary, Operator: operator, Left: &stmt.Left[0], Right: &stmt.Right[0], Span: stmt.Span}
		if left.Type.Valid() && right.Type.Valid() && !a.nativeBinaryAllowed(&expression, left, right, operator) {
			if result, found := a.resolveOperator(stmt.NodeID, operator, left, right, stmt.Span); found && result.Type.Valid() &&
				!a.info.Relations.Assignable(result.Type, left.Type).OK {
				a.addDiagnostic("semantic.operator.assignment", "operator result is not assignable to the compound assignment target", stmt.Span)
			}
		}
	}
	if stmt.Kind == ast.StmtSend {
		a.validateTypeParameterSend(stmt)
		if len(stmt.Left) == 1 && len(stmt.Right) == 1 {
			if _, elem, ok := a.info.Relations.View(a.info.Exprs[stmt.Left[0].NodeID].Type).Waitable(); ok {
				a.validateAssignments(stmt.Right, []types.TypeRef{elem})
			}
		}
	}
	if stmt.Kind == ast.StmtAssign && stmt.Op == ":=" {
		a.validateShortDeclaration(stmt.Left, scope)
		inferredTypes := a.assignmentExpressionTypes(stmt.Right, len(stmt.Left))
		for i := range stmt.Left {
			expr := &stmt.Left[i]
			if expr.Kind == ast.ExprIdent {
				if id, exists := a.info.Scopes[scope].Objects[expr.Name]; exists {
					a.info.Uses[expr.NodeID] = id
					object := a.info.Objects[id]
					a.info.Exprs[expr.NodeID] = ExprInfo{Type: object.Type, Mode: ExprValue, Object: id, Category: ValueAddressable}
				} else {
					typ := types.TypeRef{}
					if i < len(inferredTypes) {
						typ = inferredTypes[i]
					}
					a.declare(scope, ObjectVar, expr.Name, expr.NodeID, typ, true, false)
					if object, ok := a.info.Lookup(scope, expr.Name); ok {
						a.info.Exprs[expr.NodeID] = ExprInfo{Type: typ, Mode: ExprValue, Object: object.ID, Category: ValueAddressable}
					}
				}
			}
		}
	}
	if stmt.Kind == ast.StmtAssign && (stmt.Op == "=" || stmt.Op == ":=") {
		targets := make([]types.TypeRef, len(stmt.Left))
		for i, expr := range stmt.Left {
			if expr.Name != "_" {
				targets[i] = a.info.Exprs[expr.NodeID].Type
			}
		}
		a.validateAssignments(stmt.Right, targets)
	}
	if stmt.Kind == ast.StmtLabel {
		if stmt.Body.NodeID != 0 {
			a.info.NodeScopes[stmt.Body.NodeID] = scope
		}
		for i := range stmt.Body.Stmts {
			a.analyzeStmt(&stmt.Body.Stmts[i], scope)
		}
	} else {
		a.analyzeBlock(&stmt.Body, scope, true)
	}
	if stmt.Cond != nil {
		a.analyzeExpr(stmt.Cond, scope)
		if a.info.Exprs[stmt.Cond.NodeID].Mode == ExprNoValue {
			a.addDiagnostic("semantic.condition.no_value", "condition expression produces no value", stmt.Cond.Span)
		}
	}
	if stmt.Post != nil {
		a.analyzeStmt(stmt.Post, scope)
	}
	if stmt.Else != nil {
		a.analyzeStmt(stmt.Else, scope)
	}
	if stmt.Key != nil {
		a.analyzeExpr(stmt.Key, scope)
	}
	if stmt.Value != nil {
		a.analyzeExpr(stmt.Value, scope)
	}
	if stmt.Range != nil {
		a.analyzeExpr(stmt.Range, scope)
	}
	a.analyzeCaseClauses(stmt, scope)
	for i := range stmt.Results {
		a.analyzeExpr(&stmt.Results[i], scope)
		if a.info.Exprs[stmt.Results[i].NodeID].Mode == ExprNoValue {
			a.addDiagnostic("semantic.return.no_value", "return expression produces no value", stmt.Results[i].Span)
		}
	}
	if stmt.Kind == ast.StmtReturn && len(stmt.Results) != 0 {
		a.validateAssignments(stmt.Results, a.resultTypes)
	}
}

func compoundAssignmentOperator(operator string) (string, bool) {
	switch operator {
	case "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<=", ">>=", "&^=":
		return operator[:len(operator)-1], true
	default:
		return "", false
	}
}

func (a *analyzer) analyzeRangeStmt(stmt *ast.Statement, parent ScopeID) {
	scope := a.newScope(ScopeBlock, stmt.NodeID, parent)
	a.info.NodeScopes[stmt.NodeID] = scope
	if stmt.Range == nil {
		a.analyzeBlock(&stmt.Body, scope, true)
		return
	}
	a.analyzeExpr(stmt.Range, scope)
	rangeType := a.info.Exprs[stmt.Range.NodeID].Type
	keyType, valueType := a.rangeTypes(rangeType)
	if rangeType.Kind == types.TypeParameter && a.typeParameterInScope(rangeType, stmt.Range.NodeID) && !keyType.Valid() && !valueType.Valid() {
		if constraint, ok := a.info.Relations.View(rangeType).Constraint(); ok {
			if constraint.Kind == types.Any {
				a.addDiagnostic("semantic.generic.range_constraint", "type parameter constraint has no common range type", stmt.Range.Span)
			} else if _, complete := a.info.Relations.View(constraint).TypeSetTerms(); complete {
				a.addDiagnostic("semantic.generic.range_constraint", "type parameter constraint has no common range type", stmt.Range.Span)
			}
		}
	}
	if stmt.Op == ":=" {
		var left []ast.Expression
		if stmt.Key != nil {
			left = append(left, *stmt.Key)
		}
		if stmt.Value != nil {
			left = append(left, *stmt.Value)
		}
		a.validateShortDeclaration(left, scope)
		a.declareRangeTarget(stmt.Key, keyType, scope)
		a.declareRangeTarget(stmt.Value, valueType, scope)
	} else {
		a.analyzeExpr(stmt.Key, scope)
		a.analyzeExpr(stmt.Value, scope)
	}
	a.analyzeBlock(&stmt.Body, scope, true)
}

func (a *analyzer) declareRangeTarget(expr *ast.Expression, typ types.TypeRef, scope ScopeID) {
	if expr == nil || expr.Kind != ast.ExprIdent {
		return
	}
	a.info.NodeScopes[expr.NodeID] = scope
	a.declare(scope, ObjectVar, expr.Name, expr.NodeID, typ, true, false)
	if object, ok := a.info.Lookup(scope, expr.Name); ok && object.Scope == scope {
		a.info.Exprs[expr.NodeID] = ExprInfo{
			Type: typ, Mode: ExprValue, Object: object.ID,
			Category: ValueAddressable,
		}
	}
}

func (a *analyzer) rangeTypes(ref types.TypeRef) (types.TypeRef, types.TypeRef) {
	if ref.Kind == types.TypeParameter {
		terms, ok := a.typeParameterTerms(ref)
		if !ok || len(terms) == 0 {
			return types.TypeRef{}, types.TypeRef{}
		}
		first := a.info.Relations.View(terms[0])
		for _, term := range terms[1:] {
			current := a.info.Relations.View(term)
			if first.Shape() == types.Waitable && current.Shape() == types.Waitable {
				firstDir, firstElem, firstOK := first.Waitable()
				currentDir, currentElem, currentOK := current.Waitable()
				if !firstOK || !currentOK || firstDir == types.ChannelSend || currentDir == types.ChannelSend ||
					!a.info.Relations.Identical(firstElem, currentElem).OK {
					return types.TypeRef{}, types.TypeRef{}
				}
				continue
			}
			if !a.info.Relations.UnderlyingIdentical(terms[0], term).OK {
				return types.TypeRef{}, types.TypeRef{}
			}
		}
		return a.rangeTypes(terms[0])
	}
	view := a.info.Relations.View(ref)
	if _, elem, ok := view.Array(); ok {
		return types.Builtin(types.PrimitiveInt), elem
	}
	if elem, ok := view.Elem(); ok && view.Shape() == types.Slice {
		return types.Builtin(types.PrimitiveInt), elem
	}
	if key, elem, ok := view.Map(); ok {
		return key, elem
	}
	if _, elem, ok := view.Waitable(); ok {
		return elem, types.TypeRef{}
	}
	if primitive, ok := view.Primitive(); ok {
		if primitive == types.PrimitiveString {
			return types.Builtin(types.PrimitiveInt), types.Builtin(types.PrimitiveInt32)
		}
		if numeric, ok := view.NumericInfo(); ok && (numeric.Kind == types.NumericSigned || numeric.Kind == types.NumericUnsigned) {
			return types.Builtin(types.PrimitiveInt), types.TypeRef{}
		}
	}
	if signature, ok := view.Function(); ok && len(signature.Params) == 1 {
		if yield, ok := a.info.Relations.View(signature.Params[0].Type).Function(); ok {
			if len(yield.Params) == 1 {
				return yield.Params[0].Type, types.TypeRef{}
			}
			if len(yield.Params) >= 2 {
				return yield.Params[0].Type, yield.Params[1].Type
			}
		}
	}
	return types.TypeRef{}, types.TypeRef{}
}

func (a *analyzer) validateExpressionStatement(expr ast.Expression) {
	if expr.Kind == ast.ExprConvert {
		a.addDiagnostic("semantic.statement.conversion", "conversion cannot be used as an expression statement", expr.Span)
		return
	}
	if expr.Kind == ast.ExprReceive {
		return
	}
	if expr.Kind != ast.ExprCall || expr.Callee == nil {
		a.addDiagnostic("semantic.statement.expression", "expression statement must be a function call or receive operation", expr.Span)
		return
	}
	call := a.info.Calls[expr.NodeID]
	if call.Kind == CallConversion {
		a.addDiagnostic("semantic.statement.conversion", "conversion cannot be used as an expression statement", expr.Span)
		return
	}
	if call.Kind != CallBuiltin {
		return
	}
	object, ok := a.info.Object(call.Callee)
	if !ok {
		return
	}
	switch object.Name {
	case "append", "cap", "complex", "imag", "len", "make", "max", "min", "new", "real":
		a.addDiagnostic("semantic.statement.builtin_value", "result of "+object.Name+" must be used", expr.Span)
	}
}
