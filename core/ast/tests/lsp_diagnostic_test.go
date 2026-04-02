package ast_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestStructuredDiagnostics(t *testing.T) {
	code := `package main
func main() {
	a := 10
	a = "string" // Type error
}`
	conv := ffigo.NewGoToASTConverter()
	prog, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}

	validator, _ := ast.NewValidator(prog.(*ast.ProgramStmt), nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	logs := validator.Logs()
	if len(logs) == 0 {
		t.Fatal("Expected validation errors, got none")
	}

	found := false
	for _, log := range logs {
		if log.Node != nil {
			found = true
			base := log.Node.GetBase()
			if base.Loc == nil {
				t.Errorf("Error log node missing location info")
			} else {
				t.Logf("Error at %d:%d - %d:%d: %s", base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC, log.Message)
				if base.Loc.L != 4 {
					t.Errorf("Expected error at line 4, got line %d", base.Loc.L)
				}
			}
		}
	}

	if !found {
		t.Error("No error logs contained a Node reference")
	}
}

func TestPrimitiveMethodNotFound(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{
			"StringMethodNotFound",
			`package main
			func main() {
				s := "test"
				s.NotFound()
			}`,
		},
		{
			"Int64MethodNotFound",
			`package main
			func main() {
				i := 123
				i.NotFound()
			}`,
		},
		{
			"ArrayMethodNotFound",
			`package main
			func main() {
				a := []Int64{1}
				a.NotFound()
			}`,
		},
		{
			"MapMethodNotFound",
			`package main
			func main() {
				m := map[String]Int64{"a": 1}
				m.NotFound()
			}`,
		},
		{
			"PtrMethodNotFound",
			`package main
			type Point struct { X Int64 }
			func main() {
				p := Point{X: 1}
				p.NotFound()
			}`,
		},
	}

	conv := ffigo.NewGoToASTConverter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := conv.ConvertSource("snippet", tt.code)
			if err != nil {
				t.Fatal(err)
			}
			prog := node.(*ast.ProgramStmt)

			validator, _ := ast.NewValidator(prog, nil, nil, true)
			semanticCtx := ast.NewSemanticContext(validator)
			err = prog.Check(semanticCtx)

			if err == nil {
				t.Errorf("%s: Expected validation error for non-existent method, but got none", tt.name)
			}
		})
	}
}

func TestErrorRangeForMemberAccess(t *testing.T) {
	code := `package main
func main() {
	data := "NotFound"
	data.NotFound()
	fmt.Printf("1+1=%v\n", 2)
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	logs := validator.Logs()
	found := false
	for _, log := range logs {
		if log.Node != nil {
			base := log.Node.GetBase()
			if base.Loc != nil {
				t.Logf("Error at %d:%d - %d:%d: %s", base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC, log.Message)
				// 我们期望错误是在 data.NotFound() 这一行，即第 4 行
				// 并且不应该覆盖整个 main 函数或整个程序
				if base.Loc.L == 4 {
					found = true
					// 验证列范围，大致应该是 2 到 15 左右
					if base.Loc.C < 1 || base.Loc.EC > 20 {
						t.Errorf("Unexpected column range: %d:%d", base.Loc.C, base.Loc.EC)
					}
				}
			}
		}
	}

	if !found {
		t.Error("Expected precise error range at line 4 for member access, but not found or range too broad")
	}
}

func TestErrorRangeForCallArguments(t *testing.T) {
	code := `package main
func Add(a Int64, b Int64) Int64 {
	return a + b
}
func main() {
	Add(1, "2")
	Add(1)
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	logs := validator.Logs()
	foundMismatch := false
	foundTooFew := false

	for _, log := range logs {
		if log.Node != nil {
			base := log.Node.GetBase()
			if base.Loc != nil {
				t.Logf("Error at %d:%d - %d:%d: %s", base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC, log.Message)
				if base.Loc.L == 6 && log.Message == "函数第 2 个参数类型不匹配: 期望 Int64, 实际 String" {
					foundMismatch = true
				}
				if base.Loc.L == 7 && log.Message == "函数参数数量不足: 需至少 2, 实际 1" {
					foundTooFew = true
				}
			}
		}
	}

	if !foundMismatch {
		t.Error("Expected precise error range at line 6 for type mismatch, but not found")
	}
	if !foundTooFew {
		t.Error("Expected precise error range at line 7 for too few arguments, but not found")
	}
}

func TestErrorRangeForBinaryAndIncDec(t *testing.T) {
	code := `package main
func main() {
	a := 10
	a++
	b := "string"
	b++ // Error: inc/dec must be numeric
	c := 1 + "2" // Binary error (though currently BinaryExpr.Check is loose, we added WithNode)
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	_ = prog.Check(semanticCtx)

	logs := validator.Logs()
	foundIncDecError := false

	for _, log := range logs {
		if log.Node != nil {
			base := log.Node.GetBase()
			if base.Loc != nil {
				t.Logf("Error at %d:%d - %d:%d: %s", base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC, log.Message)
				if base.Loc.L == 6 && log.Message == "inc/dec 语句的操作数必须是数值类型" {
					foundIncDecError = true
				}
			}
		}
	}

	if !foundIncDecError {
		t.Error("Expected precise error range at line 6 for inc/dec error, but not found")
	}
}
