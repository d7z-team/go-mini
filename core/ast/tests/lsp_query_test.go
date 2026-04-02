package ast_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestFindNodeAt(t *testing.T) {
	code := `package main
func main() {
	a := 10
	b := a + 20
	fmt.Println(b)
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		line, col int
		expected  string // Meta type or Name
		name      string // Optional identifier name
	}{
		{2, 1, "function", ""},      // func main
		{3, 2, "identifier", "a"},   // a := 10
		{4, 2, "identifier", "b"},   // b := a + 20
		{4, 7, "identifier", "a"},   // the 'a' in a + 20
		{5, 2, "identifier", "fmt"}, // fmt.Println
		{2, 10, "function", ""},     // inside func main head
		{3, 1, "block", ""},         // inside function block, but before 'a'
	}

	for _, tt := range tests {
		node := ast.FindNodeAt(prog, tt.line, tt.col)
		if node == nil {
			t.Errorf("At %d:%d: expected %s, got nil", tt.line, tt.col, tt.expected)
			continue
		}
		meta := node.GetBase().Meta
		if tt.expected == "identifier" {
			if ident, ok := node.(*ast.IdentifierExpr); ok {
				if tt.name != "" && string(ident.Name) != tt.name {
					t.Errorf("At %d:%d: expected identifier '%s', got %s", tt.line, tt.col, tt.name, ident.Name)
				}
			} else {
				t.Errorf("At %d:%d: expected identifier, got %s (%T)", tt.line, tt.col, meta, node)
			}
		} else if meta != tt.expected {
			t.Errorf("At %d:%d: expected %s, got %s", tt.line, tt.col, tt.expected, meta)
		}
	}
}

func TestFindDefinition(t *testing.T) {
	code := `package main
var globalVar = 100
func MyFunc(param1 int) {
	localVar := param1 + globalVar
	for i := 0; i < 10; i++ {
		print(i + localVar)
	}
}
func main() {}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	parentMap := ast.BuildParentMap(prog)

	tests := []struct {
		line, col int
		name      string
		defLine   int
	}{
		{4, 14, "param1", 3},    // param1 in localVar := ...
		{4, 23, "globalVar", 2}, // globalVar in localVar := ... (in program root)
		{6, 9, "i", 5},          // i in print(i) (loop var)
		{6, 13, "localVar", 4},  // localVar in print(i + localVar)
	}

	for _, tt := range tests {
		node := ast.FindNodeAt(prog, tt.line, tt.col)
		if node == nil {
			t.Errorf("At %d:%d: node not found", tt.line, tt.col)
			continue
		}

		ident, ok := node.(*ast.IdentifierExpr)
		if !ok {
			t.Errorf("At %d:%d: expected identifier %s, got %T (%s)", tt.line, tt.col, tt.name, node, node.GetBase().Meta)
			continue
		}

		def := ast.FindDefinition(prog, ident, parentMap)
		if def == nil {
			t.Errorf("At %d:%d: definition for '%s' not found", tt.line, tt.col, ident.Name)
			continue
		}

		defBase := def.GetBase()
		if defBase.Loc.L != tt.defLine {
			t.Errorf("At %d:%d: expected definition for %s at line %d, got %d (meta=%s)", tt.line, tt.col, tt.name, tt.defLine, defBase.Loc.L, defBase.Meta)
		}
	}
}
