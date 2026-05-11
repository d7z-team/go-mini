package ast_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestStructFieldNavigation(t *testing.T) {
	code := `package main
type MyStruct struct {
	X Int64
}
func main() {
	s := MyStruct{X: 10}
	print(s.X) // Target: 7:10
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	nodeS := ast.FindNodeAt(prog, 6, 2)
	if nodeS != nil {
		t.Logf("Node at 6:2: %s (%T), Type: %s", nodeS.GetBase().Meta, nodeS, nodeS.GetBase().Type)
	}
	nodeSX := ast.FindNodeAt(prog, 7, 10)
	if nodeSX != nil {
		if m, ok := nodeSX.(*ast.MemberExpr); ok {
			t.Logf("MemberExpr at 7:10 Type: %s, Object Type: %s", m.Type, m.Object.GetBase().Type)
		}
	}

	parentMap := ast.BuildParentMap(prog)
	node := ast.FindNodeAt(prog, 7, 10)
	if node == nil {
		t.Fatal("Node at 7:10 not found")
	}
	base := node.GetBase()
	t.Logf("Node at 7:10: %s (%T) Range: %d:%d - %d:%d", base.Meta, node, base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC)

	def := ast.FindDefinition(prog, node, parentMap)
	if def == nil {
		t.Fatal("Definition of s.X not found")
	}

	if def.GetBase().Meta != "field" || def.GetBase().Loc.L != 3 || def.GetBase().Loc.C != 2 {
		t.Errorf("Expected field definition at 3:2, got %s at %d:%d", def.GetBase().Meta, def.GetBase().Loc.L, def.GetBase().Loc.C)
	}
}

func TestMethodNavigation(t *testing.T) {
	code := `package main
type S struct { X Int64 }
func (s *S) Calc() Int64 { return s.X }
func main() {
	s := S{X: 10}
	res := s.Calc() // Target: 6:11
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	parentMap := ast.BuildParentMap(prog)
	node := ast.FindNodeAt(prog, 6, 11)
	if node == nil {
		t.Fatal("Node at 6:11 not found")
	}
	t.Logf("Node at 6:11: %s (%T)", node.GetBase().Meta, node)

	def := ast.FindDefinition(prog, node, parentMap)
	if def == nil {
		t.Fatal("Definition of s.Calc not found")
	}

	if def.GetBase().Meta != "function" || def.GetBase().Loc.L != 3 {
		t.Errorf("Expected definition at line 3 (function), got line %d (%s)", def.GetBase().Loc.L, def.GetBase().Meta)
	}
}

func TestSwitchInitNavigation(t *testing.T) {
	code := `package main
func main() {
	switch v := 1; v {
	case 1:
		print(v)
	}
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	parentMap := ast.BuildParentMap(prog)
	node := ast.FindNodeAt(prog, 5, 9)
	if node == nil {
		t.Fatal("Node at 5:9 not found")
	}

	def := ast.FindDefinition(prog, node, parentMap)
	if def == nil {
		t.Fatal("Definition of switch init variable not found")
	}
	if def.GetBase().Loc.L != 3 {
		t.Fatalf("Expected switch init variable definition at line 3, got %d", def.GetBase().Loc.L)
	}
}

func TestImportedAliasReturnMemberDefinition(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	subCode := `package mymath
type Point struct {
	X Int64
	Y Int64
}
type PointAlias = Point
func MakeAlias() PointAlias { return PointAlias{X: 1, Y: 2} }`
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
	print(math.MakeAlias().Y)
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
	node := ast.FindNodeAt(mainProg, 4, 24)
	if node == nil {
		t.Fatal("Node at 4:24 not found")
	}
	def := ast.FindDefinition(mainProg, node, parentMap)
	if def == nil {
		t.Fatal("definition not found")
	}
	if def.GetBase().Meta != "field" || def.GetBase().Loc.L != 4 || def.GetBase().Loc.C != 2 {
		t.Fatalf("expected Point.Y field definition at 4:2, got %s at %d:%d", def.GetBase().Meta, def.GetBase().Loc.L, def.GetBase().Loc.C)
	}
}

func TestImportedInterfaceChainMemberDefinition(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	subCode := `package mymath
type Point struct {
	X Int64
	Y Int64
}
type Builder interface { Next() Point }
type BuilderImpl struct {}
func (b BuilderImpl) Next() Point { return Point{X: 1, Y: 2} }
func Factory() Builder { return BuilderImpl{} }`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	if err := subProg.Check(ast.NewSemanticContext(subValidator)); err != nil {
		t.Fatalf("sub module check failed: %v", err)
	}

	mainCode := `package main
import "my/math"
func main() {
	print(math.Factory().Next().Y)
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
	node := ast.FindNodeAt(mainProg, 4, 29)
	if node == nil {
		t.Fatal("Node at 4:29 not found")
	}
	def := ast.FindDefinition(mainProg, node, parentMap)
	if def == nil {
		t.Fatal("definition not found")
	}
	if def.GetBase().Meta != "field" || def.GetBase().Loc.L != 4 || def.GetBase().Loc.C != 2 {
		t.Fatalf("expected Point.Y field definition at 4:2, got %s at %d:%d", def.GetBase().Meta, def.GetBase().Loc.L, def.GetBase().Loc.C)
	}
}
