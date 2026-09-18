package parser

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/source"
)

func startSpan(token scanner.Token) source.Span {
	return token.Span
}

func join(start, end scanner.Token) source.Span {
	return spanJoin(start.Span, end.Span)
}

func spanJoin(first, second source.Span) source.Span {
	if !first.Valid() {
		return second
	}
	if !second.Valid() {
		return first
	}
	return source.Span{Start: first.Start, End: second.End}
}

func exprSpan(start scanner.Token, expr ast.Expression) source.Span {
	return spanJoin(startSpan(start), expr.Span)
}

func lastExprSpan(right, left []ast.Expression) source.Span {
	if len(right) != 0 {
		return right[len(right)-1].Span
	}
	if len(left) != 0 {
		return left[len(left)-1].Span
	}
	return source.Span{}
}
