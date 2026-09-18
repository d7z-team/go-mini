package lower

import (
	"fmt"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
)

type multiResultValue struct {
	expr  ir.Expression
	types []string
}

func (l *lowerer) expressionResultCount(expr ast.Expression, scope *funcScope) int {
	switch expr.Kind {
	case ast.ExprReceive, ast.ExprAssert:
		return 1
	case ast.ExprIndex:
		return 1
	case ast.ExprCall:
		if results, ok := l.semanticExpressionResults(expr); ok {
			return len(results)
		}
		if expr.Callee != nil {
			return l.callResultCount(*expr.Callee, scope)
		}
	}
	return 1
}

func (l *lowerer) lowerMultiResultValue(expr ast.Expression, count int, scope *funcScope, countCode string) (multiResultValue, bool) {
	value, ok := l.lowerMultiResultInitializer(expr, count, scope)
	if !ok {
		return multiResultValue{}, false
	}
	if hirResultCount(value) != count {
		l.add(countCode, "multi-result expression count does not match target count", expr.Span)
		return multiResultValue{}, false
	}
	types := make([]string, count)
	for i := range types {
		types[i] = strings.TrimSpace(l.multiResultValueType(expr, i, scope))
		if types[i] == "" {
			l.add("hirgen.result.type", "multi-result expression is missing result type metadata", expr.Span)
			return multiResultValue{}, false
		}
	}
	return multiResultValue{expr: value, types: types}, true
}

func (l *lowerer) validateMultiResultTargets(value multiResultValue, targets []string, code string, span ast.Expression) bool {
	if len(value.types) != len(targets) {
		l.add("hirgen.result.count", "multi-result expression count does not match target count", span.Span)
		return false
	}
	for i, sourceType := range value.types {
		targetType := strings.TrimSpace(targets[i])
		if targetType == "" {
			continue
		}
		relation, structured := l.semanticAssignmentRelation(span, i, targetType)
		if !structured {
			relation = l.assignmentRelation(sourceType, targetType)
		}
		if relation.OK {
			continue
		}
		l.add(code, fmt.Sprintf("cannot use result %d of type %s as %s", i+1, sourceType, targetType), span.Span)
		return false
	}
	return true
}

type multiResultBinding struct {
	producer ir.Expression
	locals   []string
	values   []ir.Expression
}

func (l *lowerer) bindMultiResult(value multiResultValue, targets []string, scope *funcScope, code string, source ast.Expression) (multiResultBinding, bool) {
	if !l.validateMultiResultTargets(value, targets, code, source) {
		return multiResultBinding{}, false
	}
	binding := multiResultBinding{
		producer: value.expr,
		locals:   make([]string, len(value.types)),
		values:   make([]ir.Expression, len(value.types)),
	}
	for i, sourceType := range value.types {
		local := l.newSyntheticLocal(scope, "result", sourceType)
		binding.locals[i] = local
		ref := ir.Expression{Kind: ir.ExprLocal, Local: local, Type: l.hirType(sourceType)}
		binding.values[i] = l.normalizeMultiResultValue(ref, sourceType, targets[i])
	}
	return binding, true
}

func (l *lowerer) normalizeMultiResultValue(value ir.Expression, sourceType, targetType string) ir.Expression {
	targetType = strings.TrimSpace(targetType)
	if targetType == "Any" && strings.TrimSpace(sourceType) != "Any" {
		return ir.Expression{Kind: ir.ExprConvert, Type: l.hirType("Any"), Operand: &value}
	}
	return value
}

func (binding multiResultBinding) capture() ir.Statement {
	targets := make([]ir.StoreTarget, len(binding.locals))
	for i, local := range binding.locals {
		targets[i] = ir.StoreTarget{Kind: "local", Local: local}
	}
	return ir.Statement{Kind: ir.StmtStoreResults, Expr: binding.producer, Targets: targets}
}

func (binding multiResultBinding) expression(values []ir.Expression) ir.Expression {
	body := ir.Expression{Kind: ir.ExprValues, Elements: append([]ir.Expression(nil), values...)}
	producer := binding.producer
	return ir.Expression{
		Kind:   ir.ExprLetResults,
		Locals: append([]string(nil), binding.locals...),
		Bind:   &producer,
		Body:   &body,
	}
}

func multiResultNeedsNormalization(sourceTypes, targetTypes []string) bool {
	for i, sourceType := range sourceTypes {
		if i < len(targetTypes) && strings.TrimSpace(targetTypes[i]) == "Any" && strings.TrimSpace(sourceType) != "Any" {
			return true
		}
	}
	return false
}
