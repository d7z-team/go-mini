package lower

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
	check "github.com/d7z-team/mini-go/compiler/semantic"
)

func (l *lowerer) lowerGoStatement(stmt ast.Statement, scope *funcScope) ([]ir.Statement, bool) {
	if stmt.Expr == nil || stmt.Expr.Kind != ast.ExprCall {
		l.add("hirgen.go.call", "go statement requires a call expression", stmt.Span)
		return nil, false
	}
	call := *stmt.Expr
	if call.Callee == nil {
		l.add("hirgen.go.callee.missing", "go call requires a callee", stmt.Span)
		return nil, false
	}
	selection, selected := l.semanticSelection(*call.Callee)
	method := selected && selection.Kind == check.SelectionMethod
	builtin := call.Callee.Kind == ast.ExprIdent && l.isBuiltinCallName(call.Callee.Name, call.Callee.Span, scope)
	if method || builtin {
		out, function, ok := l.lowerCapturedCall(call, scope, "go")
		if !ok {
			return nil, false
		}
		return append(out, ir.Statement{Kind: ir.StmtSpawn, Expr: function}), true
	}
	fn, ok := l.lowerExpression(*call.Callee, scope)
	if !ok {
		return nil, false
	}
	var args []ir.Expression
	if paramTypes, variadic, found := l.callParamTypes(*call.Callee, scope); found {
		args, ok = l.lowerCallArguments(call, scope, paramTypes, variadic, nil)
	} else if call.Ellipsis {
		l.add("hirgen.call.ellipsis", "ellipsis call requires a known Mini-Go variadic function", call.Span)
		return nil, false
	} else {
		args, ok = l.lowerExpressions(call.Args, scope)
	}
	if !ok {
		return nil, false
	}
	return []ir.Statement{{Kind: ir.StmtSpawn, Expr: fn, Args: args}}, true
}
