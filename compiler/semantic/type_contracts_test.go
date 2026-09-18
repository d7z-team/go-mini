package semantic

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/source"
)

func TestSourceTypeContracts(t *testing.T) {
	for _, tc := range []struct{ source, code string }{
		{"type A = []A", "semantic.type.alias.cycle"},
		{"type A struct { B B }; type B [1]A", "semantic.type.defined.cycle"},
		{"type A interface { B }; type B interface { A }", "semantic.interface.embed.cycle"},
		{"type A map[[]byte]int", "semantic.map.key.comparable"},
		{"type A [...]int", "semantic.type.array.infer"},
		{"type A interface { M() int }; type B interface { M() string }; type C interface { A; B }", "semantic.interface.method.conflict"},
		{"type A interface { ~int }; var a A", "semantic.general_interface.value"},
		{"type A interface { ~int | int }", "semantic.type_set.term.overlap"},
		{"func value() string { return \"x\" }; var n int = value()", "semantic.assign.type"},
		{"func value() (string, bool) { return \"x\", true }; func f() { n := 0; n, ok := value(); _ = ok; _ = n }", "semantic.assign.type"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			parsed := parser.ParseSource("example", "main.mgo", "package main\n"+tc.source)
			if source.HasErrors(parsed.Diagnostics) {
				t.Fatal(parsed.Diagnostics)
			}
			checked := Check(parsed.Program)
			for _, diagnostic := range checked.Info.Diagnostics {
				if string(diagnostic.Code) == tc.code {
					return
				}
			}
			t.Fatalf("want %s, got %v", tc.code, checked.Info.Diagnostics)
		})
	}
}

func TestSourceTypeContractsAllowIndirectRecursionAndAliases(t *testing.T) {
	parsed := parser.ParseSource("example", "main.mgo", `package main
type Node struct { Next *Node; Children []Node }
type Reader interface { Read() Reader }
type Int = int
type Numbers interface { ~Int }
type Constraint = Numbers
func Value() { type Node struct { Next *Node }; _ = Node{}; _ = [...]int{2: 1} }
`)
	checked := Check(parsed.Program)
	if source.HasErrors(parsed.Diagnostics) || source.HasErrors(checked.Info.Diagnostics) {
		t.Fatalf("parse=%v check=%v", parsed.Diagnostics, checked.Info.Diagnostics)
	}
}
