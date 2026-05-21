package ast_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/gofrontend"
)

func TestFindAllReferences(t *testing.T) {
	code := `package main
var a = 1
func main() {
	a = 2
	print(a)
	f := func() {
		print(a)
	}
	f()
}`
	conv := gofrontend.NewConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	parentMap := ast.BuildParentMap(prog)

	defNode := ast.FindNodeAt(prog, 2, 5)
	if defNode == nil {
		t.Fatal("Definition of 'a' not found at 2:5")
	}
	t.Logf("DefNode at 2:5: %s (%T)", defNode.GetBase().Meta, defNode)

	refs := ast.FindAllReferences(prog, defNode, parentMap)
	if len(refs) != 4 {
		t.Errorf("Expected 4 references to 'a', got %d", len(refs))
		for _, r := range refs {
			t.Logf("Ref at %d:%d", r.GetBase().Loc.L, r.GetBase().Loc.C)
		}
	}
}

func TestStructFieldReferences(t *testing.T) {
	code := `package main
type MyStruct struct {
	X Int64
}
func main() {
	s := MyStruct{X: 10}
	print(s.X)
	other := MyStruct{X: 20}
	print(other.X)
}`
	conv := gofrontend.NewConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	_ = prog.Check(ast.NewSemanticContext(validator))

	parentMap := ast.BuildParentMap(prog)
	node := ast.FindNodeAt(prog, 7, 10)
	if node == nil {
		t.Fatal("Node at 7:10 not found")
	}
	def := ast.FindDefinition(prog, node, parentMap)
	if def == nil {
		t.Fatal("field definition not found")
	}

	refs := ast.FindAllReferences(prog, def, parentMap)
	if len(refs) != 3 {
		t.Fatalf("expected 3 field references including definition, got %d", len(refs))
	}
}
