package scanner

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/token"
)

func TestScanSemicolonInsertionAfterTerminalTokens(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []token.Kind
	}{
		{name: "identifier", src: "x\ny", want: []token.Kind{token.Ident, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "integer EOF", src: "42", want: []token.Kind{token.Int, token.Semicolon, token.EOF}},
		{name: "float EOF", src: "1.5", want: []token.Kind{token.Float, token.Semicolon, token.EOF}},
		{name: "imaginary EOF", src: "2i", want: []token.Kind{token.Imag, token.Semicolon, token.EOF}},
		{name: "rune EOF", src: "'x'", want: []token.Kind{token.Char, token.Semicolon, token.EOF}},
		{name: "string EOF", src: `"x"`, want: []token.Kind{token.String, token.Semicolon, token.EOF}},
		{name: "break", src: "break\nx", want: []token.Kind{token.Break, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "continue", src: "continue\nx", want: []token.Kind{token.Continue, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "fallthrough", src: "fallthrough\nx", want: []token.Kind{token.Fallthrough, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "return", src: "return\nx", want: []token.Kind{token.Return, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "increment", src: "x++\ny", want: []token.Kind{token.Ident, token.Inc, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "decrement", src: "x--\ny", want: []token.Kind{token.Ident, token.Dec, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "right paren", src: "(x)\ny", want: []token.Kind{token.Lparen, token.Ident, token.Rparen, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "right bracket", src: "x[0]\ny", want: []token.Kind{token.Ident, token.Lbrack, token.Int, token.Rbrack, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "right brace", src: "{}\ny", want: []token.Kind{token.Lbrace, token.Rbrace, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
	}
	for _, tc := range cases {
		result := Scan(tc.name+".mgo", tc.src)
		if len(result.Diagnostics) != 0 {
			t.Fatalf("%s: unexpected diagnostics: %+v", tc.name, result.Diagnostics)
		}
		assertKinds(t, result.Tokens, tc.want)
	}
}

func TestScanDoesNotInsertSemicolonAfterNonTerminalTokens(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []token.Kind
	}{
		{name: "operator", src: "x +\ny", want: []token.Kind{token.Ident, token.Add, token.Ident, token.Semicolon, token.EOF}},
		{name: "comma", src: "x,\ny", want: []token.Kind{token.Ident, token.Comma, token.Ident, token.Semicolon, token.EOF}},
		{name: "left paren", src: "(\nx)", want: []token.Kind{token.Lparen, token.Ident, token.Rparen, token.Semicolon, token.EOF}},
		{name: "left bracket", src: "x[\n0]", want: []token.Kind{token.Ident, token.Lbrack, token.Int, token.Rbrack, token.Semicolon, token.EOF}},
		{name: "left brace", src: "{\nx}", want: []token.Kind{token.Lbrace, token.Ident, token.Rbrace, token.Semicolon, token.EOF}},
		{name: "keyword if", src: "if\nx", want: []token.Kind{token.If, token.Ident, token.Semicolon, token.EOF}},
	}
	for _, tc := range cases {
		result := Scan(tc.name+".mgo", tc.src)
		if len(result.Diagnostics) != 0 {
			t.Fatalf("%s: unexpected diagnostics: %+v", tc.name, result.Diagnostics)
		}
		assertKinds(t, result.Tokens, tc.want)
	}
}

func TestScanSemicolonInsertionAroundComments(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []token.Kind
	}{
		{name: "line comment", src: "x // comment\ny", want: []token.Kind{token.Ident, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "block comment without newline", src: "x /* comment */ . y", want: []token.Kind{token.Ident, token.Period, token.Ident, token.Semicolon, token.EOF}},
		{name: "block comment with newline", src: "x /* comment\ncomment */ . y", want: []token.Kind{token.Ident, token.Semicolon, token.Period, token.Ident, token.Semicolon, token.EOF}},
		{name: "block comment with multiple newlines", src: "x /* a\nb\nc */ y", want: []token.Kind{token.Ident, token.Semicolon, token.Ident, token.Semicolon, token.EOF}},
		{name: "block comment after nonterminal", src: "x + /* a\nb */ y", want: []token.Kind{token.Ident, token.Add, token.Ident, token.Semicolon, token.EOF}},
	}
	for _, tc := range cases {
		result := Scan(tc.name+".mgo", tc.src)
		if len(result.Diagnostics) != 0 {
			t.Fatalf("%s: unexpected diagnostics: %+v", tc.name, result.Diagnostics)
		}
		assertKinds(t, result.Tokens, tc.want)
	}
}
