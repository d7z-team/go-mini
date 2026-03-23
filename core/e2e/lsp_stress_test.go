package e2e

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestMultiErrorDiagnostics(t *testing.T) {
	code := `package main
type S struct { F UnknownType } // Error 1: Unknown field type
var x = 10
func main() {
	x = "string" // Error 2: Type mismatch
	y := 20
	y = true     // Error 3: Another mismatch
	UnknownFunc() // Error 4: Undefined function
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource(code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	logs := validator.Logs()

	// 我们预期至少捕获到 4 个主要错误
	expectedErrors := []string{
		"UnknownType",
		"String",
		"Bool",
		"UnknownFunc",
	}

	for i, log := range logs {
		t.Logf("Error %d: [%s] %s", i, log.Node.GetBase().Meta, log.Message)
	}

	for _, expected := range expectedErrors {
		found := false
		for _, log := range logs {
			if strings.Contains(log.Message, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected error message containing '%s' not found", expected)
		}
	}

	t.Logf("Total errors captured: %d", len(logs))
}

func TestFindNodeAt_EmptyLine(t *testing.T) {
	code := `package main
func main() {
	a := 1

	b := 2
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource(code)
	if err != nil {
		t.Fatal(err)
	}

	// 针对第 4 行（空行）进行查询
	node := ast.FindNodeAt(prog, 4, 1)
	if node == nil {
		t.Fatal("Expected node at empty line, got nil")
	}

	// 预期返回的是包含该空行的最接近的容器，即 BlockStmt
	if node.GetBase().Meta != "block" {
		t.Errorf("Expected block at empty line, got %s", node.GetBase().Meta)
	}
}

func TestFindDefinition_Shadowing(t *testing.T) {
	code := `package main
var a = 1 // Def 1 (Line 2)
func main() {
	a := 2 // Def 2 (Line 4)
	if true {
		a := 3 // Def 3 (Line 6)
		print(a) // Target 1 (Line 7)
	}
	print(a) // Target 2 (Line 9)
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource(code)
	if err != nil {
		t.Fatal(err)
	}

	parentMap := ast.BuildParentMap(prog)

	// Target 1: Line 7, Col 9 (the 'a' in print(a))
	node1 := ast.FindNodeAt(prog, 7, 9)
	def1 := ast.FindDefinition(prog, node1.(*ast.IdentifierExpr), parentMap)
	if def1.GetBase().Loc.L != 6 {
		t.Errorf("Target 1 should point to line 6, got line %d", def1.GetBase().Loc.L)
	}

	// Target 2: Line 9, Col 8 (the 'a' in print(a) outside if)
	node2 := ast.FindNodeAt(prog, 9, 8)
	def2 := ast.FindDefinition(prog, node2.(*ast.IdentifierExpr), parentMap)
	if def2.GetBase().Loc.L != 4 {
		t.Errorf("Target 2 should point to line 4, got line %d", def2.GetBase().Loc.L)
	}
}

func TestFindDefinition_ClosureCapture(t *testing.T) {
	code := `package main
func main() {
	x := 10 // Def (Line 3)
	f := func() {
		print(x) // Target (Line 5)
	}
	f()
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource(code)
	if err != nil {
		t.Fatal(err)
	}

	parentMap := ast.BuildParentMap(prog)

	// Target: Line 5, Col 9
	node := ast.FindNodeAt(prog, 5, 9)
	ident, ok := node.(*ast.IdentifierExpr)
	if !ok {
		t.Fatalf("Expected identifier at 5:9, got %T", node)
	}

	def := ast.FindDefinition(prog, ident, parentMap)
	if def == nil {
		t.Fatal("Definition not found for captured variable x")
	}
	if def.GetBase().Loc.L != 3 {
		t.Errorf("Captured x should point to line 3, got line %d", def.GetBase().Loc.L)
	}
}
