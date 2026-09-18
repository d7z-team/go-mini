package lower

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
	"github.com/d7z-team/mini-go/compiler/source"
)

func (l *lowerer) lowerMultiAssign(stmt ast.Statement, scope *funcScope) ([]ir.Statement, bool) {
	plans, ok := l.planLValues(stmt.Left, scope)
	if !ok {
		return nil, false
	}
	value, ok := l.lowerMultiResultValue(stmt.Right[0], len(stmt.Left), scope, "hirgen.assign.results")
	if !ok {
		return nil, false
	}
	targetTypes := make([]string, len(plans))
	for i, plan := range plans {
		targetTypes[i] = plan.typ
	}
	if !l.validateMultiResultTargets(value, targetTypes, "hirgen.assign.type", stmt.Right[0]) {
		return nil, false
	}
	if targets, direct := lvalueStoreTargets(plans); direct && !multiResultNeedsNormalization(value.types, targetTypes) {
		return []ir.Statement{{Kind: ir.StmtStoreResults, Expr: value.expr, Targets: targets}}, true
	}
	binding, ok := l.bindMultiResult(value, targetTypes, scope, "hirgen.assign.type", stmt.Right[0])
	if !ok {
		return nil, false
	}
	out := appendLValuePreludes(nil, plans)
	out = append(out, binding.capture())
	for i, plan := range plans {
		if store := plan.store(binding.values[i]); store.Kind != "" {
			out = append(out, store)
		}
	}
	return out, true
}

func (l *lowerer) lowerMultiResultInitializer(expr ast.Expression, targetCount int, scope *funcScope) (ir.Expression, bool) {
	if targetCount == 2 && expr.Kind == ast.ExprReceive {
		return l.lowerReceiveExpression(expr, scope, true)
	}
	if targetCount == 2 && expr.Kind == ast.ExprAssert {
		return l.lowerTypeAssertExpression(expr, scope, true)
	}
	if targetCount == 2 && expr.Kind == ast.ExprIndex {
		return l.lowerMapIndexOKExpression(expr, scope)
	}
	return l.lowerExpression(expr, scope)
}

func (l *lowerer) lowerMapIndexOKExpression(expr ast.Expression, scope *funcScope) (ir.Expression, bool) {
	if expr.Operand == nil || expr.Index == nil {
		l.add("hirgen.map.index_ok.missing", "map index-ok expression requires object and index", expr.Span)
		return ir.Expression{}, false
	}
	objectType := l.expressionType(*expr.Operand, scope)
	if _, _, ok := l.mapKeyValueTypes(objectType); !ok {
		l.add("hirgen.map.index_ok.type", "map index-ok expression requires map operand", expr.Span)
		return ir.Expression{}, false
	}
	if !l.validateMapKeyComparable(objectType, expr.Span) {
		return ir.Expression{}, false
	}
	operand, ok := l.lowerOperand(expr, scope)
	if !ok {
		return ir.Expression{}, false
	}
	index, ok := l.lowerIndexExpression(*expr.Index, l.mapIndexKeyType(*expr.Operand, scope), scope)
	if !ok {
		return ir.Expression{}, false
	}
	return ir.Expression{Kind: ir.ExprLoadIndexOK, Operand: &operand, Index: &index}, true
}

func (l *lowerer) declareLocalVars(decl ast.ValueDecl, span source.Span, scope *funcScope) bool {
	for i, name := range decl.Names {
		name = strings.TrimSpace(name)
		if name == "" || name == "_" {
			continue
		}
		if scope == nil || scope.function == nil {
			l.add("hirgen.var.scope", "local declaration requires function scope", span)
			return false
		}
		if _, exists := scope.locals[name]; exists {
			l.add("hirgen.local.duplicate", "duplicate local name", span)
			return false
		}
		if _, exists := scope.constants[name]; exists {
			l.add("hirgen.local.duplicate", "duplicate local name", span)
			return false
		}
		localType := l.localDeclType(decl, i, scope)
		variadic := l.valueDeclFunctionVariadic(decl, i, scope)
		id := l.newLocalID(scope.function, name)
		scope.function.Locals = append(scope.function.Locals, ir.Local{
			ID:          id,
			Name:        name,
			Type:        l.hirType(localType),
			Scope:       scope.debugScope,
			Declaration: hirLocationPtr(span),
		})
		scope.locals[name] = id
		scope.localTypes[name] = l.hirType(localType)
		scope.localVariadics[name] = variadic
		l.recordFunctionValueType(scope, name, localType, variadic)
	}
	return true
}

func (l *lowerer) lowerMultiValueAssign(stmt ast.Statement, scope *funcScope) ([]ir.Statement, bool) {
	if len(stmt.Left) == 0 {
		l.add("hirgen.assign.shape", "assignment emit requires at least one target", stmt.Span)
		return nil, false
	}
	plans, ok := l.planLValues(stmt.Left, scope)
	if !ok {
		return nil, false
	}
	values, ok := l.lowerExpressionsInAssignmentTypes(stmt.Right, stmt.Left, scope)
	if !ok {
		return nil, false
	}
	for _, value := range values {
		if hirResultCount(value) != 1 {
			l.add("hirgen.assign.results", "multi-valued expression must be the only assignment value", stmt.Span)
			return nil, false
		}
	}
	if len(values) != len(stmt.Left) {
		l.add("hirgen.assign.results", "assignment target count does not match expression result count", stmt.Span)
		return nil, false
	}
	if targets, direct := lvalueStoreTargets(plans); direct {
		return []ir.Statement{{Kind: ir.StmtStoreValues, Values: values, Targets: targets}}, true
	}
	return l.lowerMultiValueAssignThroughTemps(plans, values, scope), true
}

func (l *lowerer) lowerMultiValueAssignThroughTemps(plans []lvaluePlan, values []ir.Expression, scope *funcScope) []ir.Statement {
	tempTargets, tempRefs := l.lvalueResultTemps(plans, scope)
	out := appendLValuePreludes(nil, plans)
	out = append(out, ir.Statement{Kind: ir.StmtStoreValues, Values: values, Targets: tempTargets})
	for i, plan := range plans {
		store := plan.store(tempRefs[i])
		if store.Kind != "" {
			out = append(out, store)
		}
	}
	return out
}

func (l *lowerer) localStoreTargets(names []string, scope *funcScope, span source.Span) ([]ir.StoreTarget, bool) {
	targets := make([]ir.StoreTarget, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "_" {
			targets = append(targets, ir.StoreTarget{Kind: "discard"})
			continue
		}
		local, _, ok := l.lookupLocal(name, scope)
		if !ok {
			l.add("hirgen.var.local.unknown", "unknown local var declaration target", span)
			return nil, false
		}
		targets = append(targets, ir.StoreTarget{Kind: "local", Local: local})
	}
	return targets, true
}

func markLocalTargetsForRebind(targets []ir.StoreTarget, targetNames []string, names map[string]bool) {
	for i := range targets {
		if targets[i].Kind != "local" {
			continue
		}
		if names == nil || i < len(targetNames) && names[targetNames[i]] {
			targets[i].Rebind = true
		}
	}
}

func (l *lowerer) lowerStoreTarget(target ast.Expression, scope *funcScope) (ir.StoreTarget, bool) {
	if target.Kind != ast.ExprIdent {
		l.add("hirgen.assign.slot.target", "slot store target requires an identifier", target.Span)
		return ir.StoreTarget{}, false
	}
	if strings.TrimSpace(target.Name) == "_" {
		return ir.StoreTarget{Kind: "discard"}, true
	}
	if local, _, ok := l.lookupLocal(target.Name, scope); ok {
		return ir.StoreTarget{Kind: "local", Local: local}, true
	}
	if upvalue, ok := l.resolveUpvalue(target.Name, scope); ok {
		return ir.StoreTarget{Kind: "upvalue", Upvalue: upvalue}, true
	}
	if global, ok := l.globals[target.Name]; ok {
		return ir.StoreTarget{Kind: "global", Global: global}, true
	}
	l.add("hirgen.ident.unknown", "unknown assign target identifier", target.Span)
	return ir.StoreTarget{}, false
}

func loadStoreTarget(target ir.StoreTarget) (ir.Expression, bool) {
	switch target.Kind {
	case "local":
		return ir.Expression{Kind: ir.ExprLocal, Local: target.Local}, true
	case "upvalue":
		return ir.Expression{Kind: ir.ExprUpvalue, Upvalue: target.Upvalue}, true
	case "global":
		return ir.Expression{Kind: ir.ExprGlobal, Global: target.Global}, true
	default:
		return ir.Expression{}, false
	}
}

func storeTargetStatement(target ir.StoreTarget, value ir.Expression) ir.Statement {
	switch target.Kind {
	case "local":
		return ir.Statement{Kind: ir.StmtStoreLocal, Local: target.Local, Expr: value}
	case "upvalue":
		return ir.Statement{Kind: ir.StmtStoreUpvalue, Upvalue: target.Upvalue, Expr: value}
	case "global":
		return ir.Statement{Kind: ir.StmtStoreGlobal, Global: target.Global, Expr: value}
	case "discard":
		return ir.Statement{Kind: ir.StmtExpr, Expr: value}
	default:
		return ir.Statement{}
	}
}
