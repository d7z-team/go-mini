package token

import "testing"

func TestLookupAndStatementEnd(t *testing.T) {
	if got := Lookup("func"); got != Func {
		t.Fatalf("Lookup(func) = %s, want %s", got, Func)
	}
	if got := Lookup("value"); got != Ident {
		t.Fatalf("Lookup(value) = %s, want %s", got, Ident)
	}
	if !CanEndStatement(Ident) || !CanEndStatement(Return) || !CanEndStatement(Rbrace) {
		t.Fatalf("expected identifier, return and right brace to end statements")
	}
	if CanEndStatement(For) || CanEndStatement(Add) {
		t.Fatalf("for and + must not end statements")
	}
}

func TestIdentifierClassification(t *testing.T) {
	starts := []rune{'_', 'a', 'Z', '界', 'α', 'ǅ', 'ʰ'}
	for _, r := range starts {
		if !IsIdentifierStart(r) {
			t.Fatalf("%q should be accepted as identifier start", r)
		}
	}
	parts := []rune{'9', '١'}
	for _, r := range parts {
		if !IsIdentifierPart(r) {
			t.Fatalf("%q should be accepted as identifier part", r)
		}
	}
	nonStarts := []rune{'9', '١', '-', '😀', '\u0301', 'Ⅻ'}
	for _, r := range nonStarts {
		if IsIdentifierStart(r) {
			t.Fatalf("%q should not be accepted as identifier start", r)
		}
	}
	nonParts := []rune{'-', '😀', '\u0301', 'Ⅻ'}
	for _, r := range nonParts {
		if IsIdentifierPart(r) {
			t.Fatalf("%q should not be accepted as identifier part", r)
		}
	}
}
