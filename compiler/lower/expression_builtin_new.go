package lower

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
	check "github.com/d7z-team/mini-go/compiler/semantic"
)

func (l *lowerer) lowerNewBuiltin(expr ast.Expression, scope *funcScope) (ir.Expression, bool) {
	if isNilLiteral(expr.Args[0]) {
		l.add("hirgen.builtin.new.nil", "new builtin cannot allocate untyped nil", expr.Args[0].Span)
		return ir.Expression{}, false
	}
	if isTypeArgumentExpression(expr.Args[0]) {
		typ := l.resolveSourceType(expr.Args[0].Type)
		if typ == "Void" {
			l.add("hirgen.builtin.new.type", "new builtin requires a concrete type or typed expression", expr.Args[0].Span)
			return ir.Expression{}, false
		}
		local := l.newSyntheticLocal(scope, "new.object", typ)
		zero := ir.Expression{Kind: ir.ExprZero, Type: l.hirType(typ)}
		addr := ir.Expression{Kind: ir.ExprAddressOf, Local: local}
		return ir.Expression{Kind: ir.ExprLet, Local: local, Bind: &zero, Body: &addr}, true
	}
	if typ, ok := l.newNamedTypeArgument(expr.Args[0], scope); ok {
		local := l.newSyntheticLocal(scope, "new.object", typ)
		zero := ir.Expression{Kind: ir.ExprZero, Type: l.hirType(typ)}
		addr := ir.Expression{Kind: ir.ExprAddressOf, Local: local}
		return ir.Expression{Kind: ir.ExprLet, Local: local, Bind: &zero, Body: &addr}, true
	}
	targetType := l.defaultedNewExpressionType(expr.Args[0], scope)
	if strings.TrimSpace(targetType) == "" || targetType == "Void" {
		l.add("hirgen.builtin.new.type", "new builtin requires a concrete type or typed expression", expr.Args[0].Span)
		return ir.Expression{}, false
	}
	value, ok := l.lowerExpressionInType(expr.Args[0], targetType, scope)
	if !ok {
		return ir.Expression{}, false
	}
	local := l.newSyntheticLocal(scope, "new.object", targetType)
	addr := ir.Expression{Kind: ir.ExprAddressOf, Local: local}
	return ir.Expression{Kind: ir.ExprLet, Local: local, Bind: &value, Body: &addr}, true
}

func isTypeArgumentExpression(expr ast.Expression) bool {
	return expr.Kind == ast.ExprIdent && strings.TrimSpace(expr.Name) == "type" && expr.Type.Kind != ast.TypeInvalid
}

func (l *lowerer) newNamedTypeArgument(expr ast.Expression, scope *funcScope) (string, bool) {
	switch expr.Kind {
	case ast.ExprIdent:
		name := strings.TrimSpace(expr.Name)
		if name == "" || l.isValueNameBound(name, expr.Span, scope) {
			return "", false
		}
		if typ, ok := builtinTypeName(name); ok {
			return typ, true
		}
		if _, ok := l.typeDecls[name]; ok {
			return l.resolveType(name), true
		}
		if _, ok := l.typeAliases[name]; ok {
			return l.resolveType(name), true
		}
		if export, ok := l.dotImportExport(name, expr.Span); ok && export.Kind == check.ObjectType {
			return l.importedTypeCanonicalName(name, export), true
		}
	case ast.ExprSelector:
		if _, ok := l.selectorTypeExport(expr, scope); ok {
			if typ := l.selectorConversionType(expr, scope); typ != "" {
				return typ, true
			}
		}
	}
	return "", false
}

func (l *lowerer) defaultedNewExpressionType(expr ast.Expression, scope *funcScope) string {
	return l.defaultedExpressionType(expr, scope)
}
