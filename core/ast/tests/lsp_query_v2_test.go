package ast_test

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
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
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	parentMap := ast.BuildParentMap(prog)

	// 获取全局变量 a 的定义 (Line 2)
	defNode := ast.FindNodeAt(prog, 2, 5)
	if defNode == nil {
		t.Fatal("Definition of 'a' not found at 2:5")
	}
	t.Logf("DefNode at 2:5: %s (%T)", defNode.GetBase().Meta, defNode)

	refs := ast.FindAllReferences(prog, defNode, parentMap)

	// 预期引用点:
	// 1. Line 2 (定义本身目前也被计入)
	// 2. Line 4 (a = 2)
	// 3. Line 5 (print(a))
	// 4. Line 7 (print(a) in closure)
	if len(refs) != 4 {
		t.Errorf("Expected 4 references to 'a', got %d", len(refs))
		for _, r := range refs {
			t.Logf("Ref at %d:%d", r.GetBase().Loc.L, r.GetBase().Loc.C)
		}
	}
}

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

	// 重要：必须运行 Check 才能填充类型信息
	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	// 1. 测试结构体悬浮
	node1 := ast.FindNodeAt(prog, 11, 7) // "MyStruct" in main
	info1 := ast.FindHoverInfo(prog, node1, parentMap)
	if info1 == nil {
		t.Fatal("Hover info 1 is nil")
	}
	if !strings.Contains(info1.Doc, "MyStruct represents a point") {
		t.Errorf("Struct hover info mismatch, got doc: %q", info1.Doc)
	}

	// 2. 测试函数悬浮
	node2 := ast.FindNodeAt(prog, 12, 9) // "MyFunc" in main
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
	if info2.Signature != "function(Int64,Int64) Int64" {
		t.Errorf("Func signature mismatch, got: %s", info2.Signature)
	}
}

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

	// 必须运行 Check 来填充类型信息，否则 s.X 无法知道 s 是什么类型
	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	// 打印关键节点的类型以调试
	// s := MyStruct{X: 10}
	nodeS := ast.FindNodeAt(prog, 6, 2)
	if nodeS != nil {
		t.Logf("Node at 6:2: %s (%T), Type: %s", nodeS.GetBase().Meta, nodeS, nodeS.GetBase().Type)
	}
	// print(s.X)
	nodeSX := ast.FindNodeAt(prog, 7, 10)
	if nodeSX != nil {
		if m, ok := nodeSX.(*ast.MemberExpr); ok {
			t.Logf("MemberExpr at 7:10 Type: %s, Object Type: %s", m.Type, m.Object.GetBase().Type)
		}
	}

	parentMap := ast.BuildParentMap(prog)

	// 点击 s.X 处的 X
	// print(s.X)
	// 1234567890
	node := ast.FindNodeAt(prog, 7, 10)
	if node == nil {
		t.Fatal("Node at 7:10 not found")
	}
	base := node.GetBase()
	t.Logf("Node at 7:10: %s (%T) Range: %d:%d - %d:%d", base.Meta, node, base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC)

	// 如果命中 MemberExpr，看看它是如何处理的
	def := ast.FindDefinition(prog, node, parentMap)
	if def == nil {
		t.Fatal("Definition of s.X not found")
	}

	if def.GetBase().Meta != "field" || def.GetBase().Loc.L != 3 || def.GetBase().Loc.C != 2 {
		t.Errorf("Expected field definition at 3:2, got %s at %d:%d", def.GetBase().Meta, def.GetBase().Loc.L, def.GetBase().Loc.C)
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
	conv := ffigo.NewGoToASTConverter()
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

	// 点击 s.Calc() 中的 Calc (Line 6, Col 11)
	node := ast.FindNodeAt(prog, 6, 11)
	if node == nil {
		t.Fatal("Node at 6:11 not found")
	}
	t.Logf("Node at 6:11: %s (%T)", node.GetBase().Meta, node)

	def := ast.FindDefinition(prog, node, parentMap)
	if def == nil {
		t.Fatal("Definition of s.Calc not found")
	}

	// 预期指向方法实现 (Line 3)
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
