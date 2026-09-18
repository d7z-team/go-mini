package parser

import "testing"

func TestParseSemicolonOmittedBeforeClosingParenAndBrace(t *testing.T) {
	source := `package main

const (
	A = 1
	B = 2
)

func Main() int64 {
	x := int64(A + B)
	return x
}
`
	result := ParseSource("example/semicolon", "semicolon.mgo", source)
	requireNoDiagnostics(t, result)
}

func TestParseNewlineBlockCommentActsLikeSemicolon(t *testing.T) {
	source := `package main

func Main() int64 {
	x := int64(1) /*
	comment
	*/ . Missing()
	return x
	}
`
	result := ParseSource("example/semicolon", "comment.mgo", source)
	requireDiagnostic(t, result, "parser.expr")
}
