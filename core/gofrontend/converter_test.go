package gofrontend

import (
	"strings"
	"testing"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func TestCanonicalBuiltinTypeNameCoversGoIntegerAliases(t *testing.T) {
	converter := NewConverter()
	for _, name := range []string{"uint64", "uintptr"} {
		if got := converter.canonicalBuiltinTypeName(name); got != "Int64" {
			t.Fatalf("expected %s to normalize to Int64, got %s", name, got)
		}
	}
	if got := converter.canonicalBuiltinTypeName("byte"); got != "Byte" {
		t.Fatalf("expected byte to normalize to Byte, got %s", got)
	}
	if got := converter.canonicalBuiltinTypeName("rune"); got != "Rune" {
		t.Fatalf("expected rune to normalize to Rune, got %s", got)
	}
}

func TestConvertSourceNormalizesRuneLiteralsToRune(t *testing.T) {
	node, err := NewConverter().ConvertSource("rune.mgo", `package main
const Space = ' '
const Han = '你'
const Hex = '\xff'
const RawByte = '\x80'
const Small byte = 7
const Alias uint8 = 8
const Mark rune = 'A'
const Count int32 = 9

func main() {
	a := 'A'
	nl := '\n'
	wide := '\U0001F600'
	_ = a
	_ = nl
	_ = wide
}`)
	if err != nil {
		t.Fatal(err)
	}
	prog, ok := node.(*miniast.ProgramStmt)
	if !ok {
		t.Fatalf("converted node = %T, want ProgramStmt", node)
	}
	if got := prog.ConstantTypes["Space"]; got != miniast.TypeRune {
		t.Fatalf("Space type = %s, want Rune", got)
	}
	if got := prog.Constants["Space"]; got != "32" {
		t.Fatalf("Space value = %q, want 32", got)
	}
	if got := prog.Constants["Han"]; got != "20320" {
		t.Fatalf("Han value = %q, want 20320", got)
	}
	if got := prog.Constants["Hex"]; got != "255" {
		t.Fatalf("Hex value = %q, want 255", got)
	}
	if got := prog.Constants["RawByte"]; got != "128" {
		t.Fatalf("RawByte value = %q, want 128", got)
	}
	if got := prog.ConstantTypes["Small"]; got != miniast.TypeByte {
		t.Fatalf("Small type = %s, want Byte", got)
	}
	if got := prog.ConstantTypes["Alias"]; got != miniast.TypeByte {
		t.Fatalf("Alias type = %s, want Byte", got)
	}
	if got := prog.ConstantTypes["Mark"]; got != miniast.TypeRune {
		t.Fatalf("Mark type = %s, want Rune", got)
	}
	if got := prog.ConstantTypes["Count"]; got != miniast.TypeInt64 {
		t.Fatalf("Count type = %s, want Int64", got)
	}

	mainFn := prog.Functions["main"]
	if mainFn == nil || len(mainFn.Body.Children) < 3 {
		t.Fatalf("main body not converted: %#v", mainFn)
	}
	firstAssign, ok := mainFn.Body.Children[0].(*miniast.AssignmentStmt)
	if !ok {
		t.Fatalf("first stmt = %#v, want assignment", mainFn.Body.Children[0])
	}
	firstLit, ok := firstAssign.Value.(*miniast.LiteralExpr)
	if !ok || firstLit.Type != miniast.TypeRune || firstLit.Value != "65" {
		t.Fatalf("first literal = %#v, want Rune 65", firstAssign.Value)
	}
	secondAssign, ok := mainFn.Body.Children[1].(*miniast.AssignmentStmt)
	if !ok {
		t.Fatalf("second stmt = %#v, want assignment", mainFn.Body.Children[1])
	}
	secondLit, ok := secondAssign.Value.(*miniast.LiteralExpr)
	if !ok || secondLit.Type != miniast.TypeRune || secondLit.Value != "10" {
		t.Fatalf("second literal = %#v, want Rune 10", secondAssign.Value)
	}
	thirdAssign, ok := mainFn.Body.Children[2].(*miniast.AssignmentStmt)
	if !ok {
		t.Fatalf("third stmt = %#v, want assignment", mainFn.Body.Children[2])
	}
	thirdLit, ok := thirdAssign.Value.(*miniast.LiteralExpr)
	if !ok || thirdLit.Type != miniast.TypeRune || thirdLit.Value != "128512" {
		t.Fatalf("third literal = %#v, want Rune 128512", thirdAssign.Value)
	}
}

func TestConvertSourceReportsUnsupportedConstructsWithoutPanic(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{
			name: "new string literal type",
			code: `package main
func main() {
	_ = new("Int64")
}`,
			want: "new first argument must be a type",
		},
		{
			name: "range key index target",
			code: `package main
func main() {
	arr := []Int64{1}
	for arr[0] = range arr {}
}`,
			want: "range key target must be an identifier",
		},
		{
			name: "fallthrough",
			code: `package main
func main() {
	switch 1 {
	case 1:
		fallthrough
	}
}`,
			want: "fallthrough",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ConvertSource panicked: %v", r)
				}
			}()
			_, err := NewConverter().ConvertSource("bad.mgo", tc.code)
			if err == nil {
				t.Fatal("expected conversion error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}
