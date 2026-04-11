package ast_test

import (
	"fmt"
	"strings"
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
					if base.Meta != "literal" {
						t.Fatalf("expected mismatch diagnostic on argument literal, got %s", base.Meta)
					}
					foundMismatch = true
				}
				if base.Loc.L == 7 && log.Message == "函数参数数量不足: 需至少 2, 实际 1" {
					if base.Meta != "call" {
						t.Fatalf("expected arity diagnostic on call node, got %s", base.Meta)
					}
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

func TestDiagnosticDeduplicateSameNode(t *testing.T) {
	code := `package main
func main() {
	bad := badCall()
	print(bad[unknown])
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSourceTolerant("snippet", code)
	if len(err) > 0 {
		t.Fatalf("unexpected tolerant parse errors: %v", err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	_ = prog.Check(ast.NewSemanticContext(validator))

	seen := make(map[string]int)
	for _, log := range validator.Logs() {
		if log.Node == nil || log.Node.GetBase() == nil || log.Node.GetBase().Loc == nil {
			continue
		}
		key := fmt.Sprintf("%s|%s|%d|%d", log.Message, log.Node.GetBase().Meta, log.Node.GetBase().Loc.L, log.Node.GetBase().Loc.C)
		seen[key]++
		if seen[key] > 1 {
			t.Fatalf("duplicated diagnostic found: %q at %d:%d", log.Message, log.Node.GetBase().Loc.L, log.Node.GetBase().Loc.C)
		}
	}
}

func TestErrorRangeForIndexAndSliceOperands(t *testing.T) {
	code := `package main
func main() {
	arr := []Int64{1, 2, 3}
	_ = arr["0"]
	_ = arr[1:"2"]
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	_ = prog.Check(ast.NewSemanticContext(validator))

	var foundIndex bool
	var foundSlice bool
	for _, log := range validator.Logs() {
		if log.Node == nil || log.Node.GetBase() == nil || log.Node.GetBase().Loc == nil {
			continue
		}
		base := log.Node.GetBase()
		switch log.Message {
		case "数组索引只支持 Int64 类型 (String)":
			if base.Meta != "literal" || base.Loc.L != 4 {
				t.Fatalf("expected index diagnostic on literal at line 4, got %s at %d", base.Meta, base.Loc.L)
			}
			foundIndex = true
		case "slice high 索引必须是数值类型":
			if base.Meta != "literal" || base.Loc.L != 5 {
				t.Fatalf("expected slice diagnostic on literal at line 5, got %s at %d", base.Meta, base.Loc.L)
			}
			foundSlice = true
		}
	}
	if !foundIndex {
		t.Fatal("expected precise index operand diagnostic")
	}
	if !foundSlice {
		t.Fatal("expected precise slice operand diagnostic")
	}
}

func TestErrorRangeForAssignmentValue(t *testing.T) {
	code := `package main
func main() {
	var a Int64
	a = "x"
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	_ = prog.Check(ast.NewSemanticContext(validator))

	for _, log := range validator.Logs() {
		if log.Message != "类型不匹配: 无法将 String 赋值给 a (Int64)" {
			continue
		}
		if log.Node == nil || log.Node.GetBase() == nil || log.Node.GetBase().Loc == nil {
			t.Fatal("assignment diagnostic missing node")
		}
		base := log.Node.GetBase()
		if base.Meta != "literal" || base.Loc.L != 4 {
			t.Fatalf("expected assignment diagnostic on value literal at line 4, got %s at %d", base.Meta, base.Loc.L)
		}
		return
	}
	t.Fatal("expected assignment mismatch diagnostic")
}

func TestErrorRangeForBinaryAndIncDec(t *testing.T) {
	code := `package main
func main() {
	a := 10
	a++
	b := "string"
	b++ // Error: inc/dec must be numeric
	c := 1 && true
	d := !1
	e := 1 << "2"
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
	foundBinaryBoolError := false
	foundUnaryError := false
	foundBitwiseError := false

	for _, log := range logs {
		if log.Node != nil {
			base := log.Node.GetBase()
			if base.Loc != nil {
				t.Logf("Error at %d:%d - %d:%d: %s", base.Loc.L, base.Loc.C, base.Loc.EL, base.Loc.EC, log.Message)
				if base.Loc.L == 6 && log.Message == "inc/dec 语句的操作数必须是数值类型" {
					if base.Meta != "identifier" {
						t.Fatalf("expected inc/dec diagnostic on operand identifier, got %s", base.Meta)
					}
					foundIncDecError = true
				}
				if base.Loc.L == 7 && log.Message == "And 运算符预期 Bool, 实际为 Int64" {
					if base.Meta != "literal" {
						t.Fatalf("expected binary bool diagnostic on left literal, got %s", base.Meta)
					}
					foundBinaryBoolError = true
				}
				if base.Loc.L == 8 && log.Message == "Not 运算符预期 Bool, 实际为 Int64" {
					if base.Meta != "literal" {
						t.Fatalf("expected unary diagnostic on operand literal, got %s", base.Meta)
					}
					foundUnaryError = true
				}
				if base.Loc.L == 9 && log.Message == "Lsh 运算符预期 Int64, 实际为 String" {
					if base.Meta != "literal" {
						t.Fatalf("expected shift diagnostic on right literal, got %s", base.Meta)
					}
					foundBitwiseError = true
				}
			}
		}
	}

	if !foundIncDecError {
		t.Error("Expected precise error range at line 6 for inc/dec error, but not found")
	}
	if !foundBinaryBoolError {
		t.Error("Expected precise error range at line 7 for binary bool error, but not found")
	}
	if !foundUnaryError {
		t.Error("Expected precise error range at line 8 for unary error, but not found")
	}
	if !foundBitwiseError {
		t.Error("Expected precise error range at line 9 for shift error, but not found")
	}
}

func TestDiagnosticDeduplicatePrefersNarrowerRange(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
	}
	validator, err := ast.NewValidator(prog, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	parent := &ast.ExpressionStmt{BaseNode: ast.BaseNode{Meta: "expr_stmt", Loc: &ast.Position{F: "snippet", L: 2, C: 2, EL: 2, EC: 12}}}
	child := &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "String", Loc: &ast.Position{F: "snippet", L: 2, C: 8, EL: 2, EC: 10}}, Value: "x"}

	ctx := ast.NewSemanticContext(validator).WithNode(parent)
	ctx.AddErrorf("same root cause")
	ctx.AddErrorAt(child, "same root cause")

	logs := validator.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 deduplicated diagnostic, got %d", len(logs))
	}
	if logs[0].Node == nil || logs[0].Node.GetBase() == nil || logs[0].Node.GetBase().Meta != "literal" {
		t.Fatalf("expected narrower literal diagnostic to be kept, got %+v", logs[0].Node)
	}
}

func TestStructuredForwardingRemovesIfWrapperMessage(t *testing.T) {
	code := `package main
func main() {
	if badCall() {
		print(1)
	}
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	_ = prog.Check(ast.NewSemanticContext(validator))

	for _, log := range validator.Logs() {
		if strings.Contains(log.Message, "IfStmt Cond.Check error") {
			t.Fatalf("unexpected wrapped if diagnostic: %s", log.Message)
		}
	}
}

func TestStructuredForwardingKeepsReturnPathDiagnosticWithoutGenericWrapper(t *testing.T) {
	code := `package main
func bad(a Bool) Int64 {
	if a {
		return 1
	}
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	_ = prog.Check(ast.NewSemanticContext(validator))

	var foundSpecific bool
	for _, log := range validator.Logs() {
		if log.Message == "函数缺少返回语句或并非所有分支都有返回语句" {
			foundSpecific = true
		}
		if strings.Contains(log.Message, "函数 bad 缺少返回语句") {
			t.Fatalf("unexpected generic function wrapper diagnostic: %s", log.Message)
		}
	}
	if !foundSpecific {
		t.Fatal("expected return path diagnostic")
	}
}

func TestStructuredForwardingProgramBlockFunctionFallbacks(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{F: "snippet", L: 1, C: 1, EL: 4, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{F: "snippet", L: 1, C: 12, EL: 4, EC: 1}},
					Children: []ast.Stmt{
						&ast.ExpressionStmt{
							BaseNode: ast.BaseNode{Meta: "expr_stmt", Loc: &ast.Position{F: "snippet", L: 2, C: 2, EL: 2, EC: 12}},
							X: &ast.ImportExpr{
								BaseNode: ast.BaseNode{Meta: "import", Loc: &ast.Position{F: "snippet", L: 2, C: 2, EL: 2, EC: 12}},
								Path:     "x/y",
							},
						},
					},
				},
			},
		},
	}

	validator, err := ast.NewValidator(prog, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = prog.Check(ast.NewSemanticContext(validator))

	logs := validator.Logs()
	if len(logs) == 0 {
		t.Fatal("expected fallback diagnostic")
	}
	found := false
	for _, log := range logs {
		if log.Message != "import 只能在包级作用域中使用" {
			continue
		}
		if log.Node == nil || log.Node.GetBase() == nil {
			t.Fatal("fallback diagnostic missing node")
		}
		if log.Node.GetBase().Meta != "import" {
			t.Fatalf("expected fallback to attach to import child node, got %s", log.Node.GetBase().Meta)
		}
		found = true
	}
	if !found {
		t.Fatal("expected import scope diagnostic")
	}
}
