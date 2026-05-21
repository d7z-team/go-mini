package ast_test

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestFindHoverInfo(t *testing.T) {
	code := `package main
// MyStruct represents a point
type MyStruct struct { X Int64 }

// MyFunc adds two numbers
func MyFunc(a Int64, b Int64) Int64 {
	return a + b
}

func main() {
	s := MyStruct{X: 10}
	res := MyFunc(1, 2)
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	parentMap := ast.BuildParentMap(prog)

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	node1 := ast.FindNodeAt(prog, 11, 7)
	info1 := ast.FindHoverInfo(prog, node1, parentMap)
	if info1 == nil {
		t.Fatal("Hover info 1 is nil")
	}
	if !strings.Contains(info1.Doc, "MyStruct represents a point") {
		t.Errorf("Struct hover info mismatch, got doc: %q", info1.Doc)
	}

	node2 := ast.FindNodeAt(prog, 12, 9)
	if node2 != nil {
		t.Logf("Node at 12:9: %s (%T)", node2.GetBase().Meta, node2)
	}
	info2 := ast.FindHoverInfo(prog, node2, parentMap)
	if info2 == nil {
		t.Fatal("Hover info 2 is nil")
	}
	if !strings.Contains(info2.Doc, "MyFunc adds two numbers") {
		t.Errorf("Func hover info mismatch, got doc: %q", info2.Doc)
	}
	if info2.Signature != "function(Int64, Int64) Int64" {
		t.Errorf("Func signature mismatch, got: %s", info2.Signature)
	}
}

func TestImportedTupleReturnMemberHover(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	subCode := `package mymath
type Point struct { X Int64; Y Int64 }
func SplitPoint() (Point, Bool) { return Point{X: 1, Y: 2}, true }`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	mainCode := `package main
import "my/math"
func main() {
	print(math.SplitPoint().Y)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)
	validator, _ := ast.NewValidator(mainProg, nil, nil, true)
	validator.Root().ImportedRoots["my/math"] = subValidator.Root()
	validator.Root().DiscoverImportedRoot("my/math")
	_ = mainProg.Check(ast.NewSemanticContext(validator))

	parentMap := ast.BuildParentMap(mainProg)
	node := ast.FindNodeAt(mainProg, 4, 25)
	if node == nil {
		t.Fatal("Node at 4:25 not found")
	}
	info := ast.FindHoverInfo(mainProg, node, parentMap)
	if info == nil {
		t.Fatal("hover info is nil")
	}
	if info.Type != "Int64" {
		t.Fatalf("expected Int64 hover type, got %s", info.Type)
	}
	if !strings.Contains(info.Signature, "field Y Int64") {
		t.Fatalf("expected field signature for Y, got %q", info.Signature)
	}
}
