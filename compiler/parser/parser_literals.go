package parser

import (
	"strconv"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/token"
)

func (p *parser) literalExpression(scanned scanner.Token) ast.Expression {
	text := strings.ReplaceAll(scanned.Lexeme, "_", "")
	typ := literalType(scanned)
	switch scanned.Kind {
	case token.Int, token.Float, token.Imag:
		// Numeric source spelling remains exact until compiler constant evaluation.
		return ast.Expression{Kind: ast.ExprLiteral, Span: scanned.Span, Literal: text, Type: typ}
	case token.String:
		value, err := strconv.Unquote(scanned.Lexeme)
		if err != nil {
			p.add("parser.literal.string", "invalid string literal", scanned.Span)
			return ast.Expression{Kind: ast.ExprInvalid, Span: scanned.Span}
		}
		return ast.Expression{Kind: ast.ExprLiteral, Span: scanned.Span, Literal: strconv.Quote(value), Type: typ}
	case token.Char:
		literal := scanned.Lexeme
		if len(literal) < 2 {
			p.add("parser.literal.char", "invalid char literal", scanned.Span)
			return ast.Expression{Kind: ast.ExprInvalid, Span: scanned.Span}
		}
		value, _, tail, err := strconv.UnquoteChar(literal[1:len(literal)-1], '\'')
		if err != nil || tail != "" {
			p.add("parser.literal.char", "invalid char literal", scanned.Span)
			return ast.Expression{Kind: ast.ExprInvalid, Span: scanned.Span}
		}
		return ast.Expression{Kind: ast.ExprLiteral, Span: scanned.Span, Literal: strconv.FormatInt(int64(value), 10), Type: typ}
	default:
		return ast.Expression{Kind: ast.ExprLiteral, Span: scanned.Span, Literal: scanned.Lexeme, Type: typ}
	}
}

func literalType(scanned scanner.Token) ast.TypeExpr {
	switch scanned.Kind {
	case token.String:
		return ast.TypeExpr{Kind: ast.TypeName, Name: "String", Span: scanned.Span}
	case token.Int:
		return ast.TypeExpr{Kind: ast.TypeName, Name: "Int", Span: scanned.Span}
	case token.Float:
		return ast.TypeExpr{Kind: ast.TypeName, Name: "Float64", Span: scanned.Span}
	case token.Imag:
		return ast.TypeExpr{Kind: ast.TypeName, Name: "Complex128", Span: scanned.Span}
	case token.Char:
		return ast.TypeExpr{Kind: ast.TypeName, Name: "Int32", Span: scanned.Span}
	default:
		return ast.TypeExpr{Kind: ast.TypeName, Name: "Any", Span: scanned.Span}
	}
}

func replaceIotaExpressions(expressions []ast.Expression, value int) []ast.Expression {
	out := make([]ast.Expression, 0, len(expressions))
	for _, expr := range expressions {
		out = append(out, replaceIotaExpression(expr, value))
	}
	return out
}

func replaceIotaExpression(expr ast.Expression, value int) ast.Expression {
	if expr.Kind == ast.ExprIdent && expr.Name == "iota" {
		return ast.Expression{
			Kind:    ast.ExprLiteral,
			Span:    expr.Span,
			Literal: strconv.Itoa(value),
			Type:    ast.TypeExpr{Kind: ast.TypeName, Name: "Int", Span: expr.Span},
		}
	}
	if expr.Left != nil {
		left := replaceIotaExpression(*expr.Left, value)
		expr.Left = &left
	}
	if expr.Right != nil {
		right := replaceIotaExpression(*expr.Right, value)
		expr.Right = &right
	}
	if expr.Operand != nil {
		operand := replaceIotaExpression(*expr.Operand, value)
		expr.Operand = &operand
	}
	if expr.Callee != nil {
		callee := replaceIotaExpression(*expr.Callee, value)
		expr.Callee = &callee
	}
	for i := range expr.Args {
		expr.Args[i] = replaceIotaExpression(expr.Args[i], value)
	}
	if expr.Index != nil {
		index := replaceIotaExpression(*expr.Index, value)
		expr.Index = &index
	}
	if expr.Start != nil {
		start := replaceIotaExpression(*expr.Start, value)
		expr.Start = &start
	}
	if expr.End != nil {
		end := replaceIotaExpression(*expr.End, value)
		expr.End = &end
	}
	if expr.Max != nil {
		maxExpr := replaceIotaExpression(*expr.Max, value)
		expr.Max = &maxExpr
	}
	for i := range expr.Elements {
		expr.Elements[i] = replaceIotaExpression(expr.Elements[i], value)
	}
	for i := range expr.Entries {
		if expr.Entries[i].Key != nil {
			key := replaceIotaExpression(*expr.Entries[i].Key, value)
			expr.Entries[i].Key = &key
		}
		expr.Entries[i].Value = replaceIotaExpression(expr.Entries[i].Value, value)
	}
	for i := range expr.Items {
		if expr.Items[i].Key != nil {
			key := replaceIotaExpression(*expr.Items[i].Key, value)
			expr.Items[i].Key = &key
		}
		expr.Items[i].Value = replaceIotaExpression(expr.Items[i].Value, value)
	}
	for i := range expr.Func.Body.Stmts {
		expr.Func.Body.Stmts[i] = replaceIotaStatement(expr.Func.Body.Stmts[i], value)
	}
	return expr
}

func replaceIotaStatement(stmt ast.Statement, value int) ast.Statement {
	if stmt.Expr != nil {
		expr := replaceIotaExpression(*stmt.Expr, value)
		stmt.Expr = &expr
	}
	for i := range stmt.Left {
		stmt.Left[i] = replaceIotaExpression(stmt.Left[i], value)
	}
	for i := range stmt.Right {
		stmt.Right[i] = replaceIotaExpression(stmt.Right[i], value)
	}
	if stmt.Init != nil {
		init := replaceIotaStatement(*stmt.Init, value)
		stmt.Init = &init
	}
	if stmt.Cond != nil {
		cond := replaceIotaExpression(*stmt.Cond, value)
		stmt.Cond = &cond
	}
	if stmt.Post != nil {
		post := replaceIotaStatement(*stmt.Post, value)
		stmt.Post = &post
	}
	if stmt.Else != nil {
		elseStmt := replaceIotaStatement(*stmt.Else, value)
		stmt.Else = &elseStmt
	}
	if stmt.Range != nil {
		rangeExpr := replaceIotaExpression(*stmt.Range, value)
		stmt.Range = &rangeExpr
	}
	for i := range stmt.Body.Stmts {
		stmt.Body.Stmts[i] = replaceIotaStatement(stmt.Body.Stmts[i], value)
	}
	for i := range stmt.Cases {
		stmt.Cases[i] = replaceIotaCase(stmt.Cases[i], value)
	}
	for i := range stmt.Results {
		stmt.Results[i] = replaceIotaExpression(stmt.Results[i], value)
	}
	return stmt
}

func replaceIotaCase(clause ast.CaseClause, value int) ast.CaseClause {
	for i := range clause.Values {
		clause.Values[i] = replaceIotaExpression(clause.Values[i], value)
	}
	if clause.Comm != nil {
		comm := replaceIotaStatement(*clause.Comm, value)
		clause.Comm = &comm
	}
	for i := range clause.Body.Stmts {
		clause.Body.Stmts[i] = replaceIotaStatement(clause.Body.Stmts[i], value)
	}
	return clause
}

func calleeType(callee ast.Expression) (ast.TypeExpr, bool) {
	if callee.Kind == ast.ExprIdent && callee.Name == "type" && callee.Type.Kind != ast.TypeInvalid {
		return callee.Type, true
	}
	return ast.TypeExpr{}, false
}

func isPredeclaredTypeName(name string) bool {
	switch name {
	case "bool", "string",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune",
		"float32", "float64",
		"complex64", "complex128",
		"any", "error":
		return true
	default:
		return false
	}
}

func exprAsType(expr ast.Expression) (ast.TypeExpr, bool) {
	switch expr.Kind {
	case ast.ExprIdent:
		if expr.Name == "type" && expr.Type.Kind != ast.TypeInvalid {
			return expr.Type, true
		}
		return ast.TypeExpr{Kind: ast.TypeName, Name: expr.Name, Span: expr.Span}, true
	case ast.ExprSelector:
		if expr.Operand != nil && expr.Operand.Kind == ast.ExprIdent {
			name := expr.Operand.Name + "." + expr.Field
			return ast.TypeExpr{Kind: ast.TypeName, Name: name, Span: expr.Span}, true
		}
	case ast.ExprIndex, ast.ExprIndexList:
		if expr.Operand == nil {
			return ast.TypeExpr{}, false
		}
		base, ok := exprAsType(*expr.Operand)
		if !ok {
			return ast.TypeExpr{}, false
		}
		var expressions []ast.Expression
		if expr.Kind == ast.ExprIndex && expr.Index != nil {
			expressions = []ast.Expression{*expr.Index}
		} else {
			expressions = expr.Args
		}
		args := make([]ast.TypeExpr, 0, len(expressions))
		for _, expression := range expressions {
			arg, ok := exprAsType(expression)
			if !ok {
				return ast.TypeExpr{}, false
			}
			args = append(args, arg)
		}
		if len(args) == 0 {
			return ast.TypeExpr{}, false
		}
		return ast.TypeExpr{Kind: ast.TypeInstance, Base: &base, TypeArgs: args, Span: expr.Span}, true
	}
	return ast.TypeExpr{}, false
}

func panicCall(expr ast.Expression) (*ast.Expression, bool) {
	if expr.Kind != ast.ExprCall || expr.Callee == nil || len(expr.Args) != 1 {
		return nil, false
	}
	if expr.Callee.Kind != ast.ExprIdent || expr.Callee.Name != "panic" {
		return nil, false
	}
	arg := expr.Args[0]
	return &arg, true
}
