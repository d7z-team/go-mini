package scanner

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/token"
)

func TestScanUnicodeIdentifierCategories(t *testing.T) {
	result := Scan("unicode.mgo", "package main\nvar 变量١ = αβ + ǅelta + ʰelper\n")
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	for _, lexeme := range []string{"变量١", "αβ", "ǅelta", "ʰelper"} {
		if !hasLexeme(result.Tokens, token.Ident, lexeme) {
			t.Fatalf("expected identifier %q, got %#v", lexeme, result.Tokens)
		}
	}
}

func TestScanRejectsInvalidUnicodeIdentifierCharacters(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "unicode digit start", src: "١abc"},
		{name: "emoji", src: "x😀"},
		{name: "combining mark", src: "x\u0301"},
		{name: "nondecimal number", src: "xⅫ"},
	}
	for _, tc := range cases {
		result := Scan(tc.name+".mgo", tc.src)
		if !hasDiagnostic(result.Diagnostics, "scanner.token.illegal") {
			t.Fatalf("%s: expected scanner.token.illegal, got %+v", tc.name, result.Diagnostics)
		}
	}
}
