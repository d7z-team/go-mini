package e2e

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestConvertSourceTolerant_VariableRetention(t *testing.T) {
	// var a = 10; b := a + (语法错误)
	code := `package main
func main() {
	var a = 10
	b := a + 
}`
	conv := ffigo.NewGoToASTConverter()
	prog, errs := conv.ConvertSourceTolerant(code)

	if len(errs) == 0 {
		t.Fatal("Expected syntax errors, got none")
	}

	program := prog.(*ast.ProgramStmt)

	// 验证 main 函数中的语句
	// 注意：go/parser 遇到这种情况可能会把后面的 b := 20 也解析错，或者丢弃
	// 我们检查 program.Functions["main"] 或者 program.Main

	mainFunc, ok := program.Functions["main"]
	if !ok {
		t.Fatal("main function not found")
	}

	foundA := false
	foundB := false
	v := &testVisitor{
		onVisit: func(node ast.Node) {
			if assign, ok := node.(*ast.AssignmentStmt); ok {
				if ident, ok := assign.LHS.(*ast.IdentifierExpr); ok {
					if string(ident.Name) == "a" {
						foundA = true
					}
					if string(ident.Name) == "b" {
						foundB = true
						// b := a + (里面应该包含 BadExpr)
						hasBad := false
						ast.Walk(&testVisitor{onVisit: func(n ast.Node) {
							if _, ok := n.(*ast.BadExpr); ok {
								hasBad = true
							}
						}}, assign.Value)
						if !hasBad {
							t.Errorf("Expected some BadExpr within variable 'b' value, but none found in %T", assign.Value)
						}
					}
				}
			}
		},
	}
	ast.Walk(v, mainFunc.Body)

	if !foundA {
		t.Error("Variable 'a' was lost")
	}
	if !foundB {
		t.Error("Variable 'b' was lost despite tolerant mode")
	}
}

type testVisitor struct {
	onVisit func(ast.Node)
}

func (v *testVisitor) Visit(node ast.Node) ast.Visitor {
	v.onVisit(node)
	return v
}
