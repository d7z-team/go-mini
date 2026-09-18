package parser

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
)

func TestParseNoSwitchFunctionDirective(t *testing.T) {
	result := ParseSource("example", "sync.mgo", `package example

// update changes one value without scheduler rotation.
//minigo:noswitch
func update() {}

type counter struct{}

//minigo:noswitch
func (counter) reset() {}
`)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	var functions []ast.FuncDecl
	for _, decl := range result.Program.Files[0].Decls {
		if decl.Kind == ast.DeclFunc {
			functions = append(functions, decl.Func)
		}
	}
	if len(functions) != 2 || !functions[0].NoSwitch || !functions[1].NoSwitch {
		t.Fatalf("no-switch functions = %#v", functions)
	}
}

func TestParseNoSwitchDirectiveReportsInvalidPlacement(t *testing.T) {
	tests := []string{
		"package example\n//minigo:noswitch extra\nfunc update() {}\n",
		"package example\n//minigo:noswitch\nvar value = 1\n",
		"package example\nvar update = //minigo:noswitch\nfunc() {}\n",
	}
	for _, source := range tests {
		result := ParseSource("example", "invalid.mgo", source)
		found := false
		for _, diagnostic := range result.Diagnostics {
			found = found || diagnostic.Code == "parser.noswitch.directive" || diagnostic.Code == "parser.noswitch.declaration"
		}
		if !found {
			t.Fatalf("missing no-switch diagnostic for %q: %#v", source, result.Diagnostics)
		}
	}
}
