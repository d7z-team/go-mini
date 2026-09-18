package parser

import "testing"

func TestParseSourcePreservesExactNumericLiteralSpelling(t *testing.T) {
	result := ParseSource("example/main", "main.mgo", `package main
func Main() {
	_ = 0xffffffffffffffffffffffffffffffff
	_ = 1_000_000.000_001e+200
	_ = 0x1.fffffffffffffp1023
	_ = 9007199254740993.0i
	_ = .5
}
`)
	requireNoDiagnostics(t, result)
	body := result.Program.Files[0].Decls[0].Func.Body.Stmts
	want := []string{
		"0xffffffffffffffffffffffffffffffff",
		"1000000.000001e+200",
		"0x1.fffffffffffffp1023",
		"9007199254740993.0i",
		".5",
	}
	for i, expected := range want {
		if got := body[i].Right[0].Literal; got != expected {
			t.Fatalf("literal %d = %q, want %q", i, got, expected)
		}
	}
}
