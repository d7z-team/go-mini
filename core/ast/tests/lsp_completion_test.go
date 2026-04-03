package ast_test

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestUnimportedFFIValidation(t *testing.T) {
	code := `package main
func main() {
	os.ReadFile("test.txt")
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	// 模拟 FFI 符号
	externalSpecs := map[ast.Ident]ast.GoMiniType{
		"os.ReadFile": "function(String) (TypeBytes, String)",
	}

	validator, _ := ast.NewValidator(prog, externalSpecs, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	err = prog.Check(semanticCtx)
	// 预期没有验证错误，因为 os 应该被自动识别为 Package
	if err != nil {
		t.Errorf("Expected no validation error for unimported FFI package, got: %v", err)
		for _, log := range validator.Logs() {
			t.Logf("Log: %s", log.Message)
		}
	}
}

func TestUnimportedFFICompletion(t *testing.T) {
	// 使用完整的成员表达式，然后在成员位置尝试补全
	code := `package main
func main() {
	os.ReadFile("test")
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	externalSpecs := map[ast.Ident]ast.GoMiniType{
		"os.ReadFile":  "function(String) (TypeBytes, String)",
		"os.WriteFile": "function(String, TypeBytes) String",
	}

	ast.NewValidator(prog, externalSpecs, nil, true)
	// 在 "os." 之后触发补全
	// Line 3, Col 5 是 "o", Col 6 是 "s", Col 7 是 "."
	completions := ast.FindCompletionsAt(prog, 3, 7)

	foundReadFile := false
	foundWriteFile := false
	for _, item := range completions {
		if item.Label == "ReadFile" {
			foundReadFile = true
		}
		if item.Label == "WriteFile" {
			foundWriteFile = true
		}
	}

	if !foundReadFile || !foundWriteFile {
		t.Errorf("Expected 'ReadFile' and 'WriteFile' in completions, got: %+v", completions)
	}
}

func TestGoSourceModuleCompletion(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	// 模拟导入的子模块
	subCode := `package mymath
func Add(a Int64, b Int64) Int64 { return a + b }
type Point struct { X Int64 }
`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)

	mainCode := `package main
import "my/math"
func main() {
	math.Add(1, 2)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(mainProg, nil, nil, true)

	// 手动注入子模块 Root (模拟 Loader 行为之后的结果)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	validator.Root().ImportedRoots["my/math"] = subValidator.Root()
	// 注意：converter 已经根据 import "my/math" 建立了 math -> my/math 的映射

	// 在 "math." 之后触发补全 (Line 4, Col 6 是 '.', 尝试 Col 6 或 Col 7)
	completions := ast.FindCompletionsAt(mainProg, 4, 6)

	foundAdd := false
	foundPoint := false
	for _, item := range completions {
		if item.Label == "Add" {
			foundAdd = true
		}
		if item.Label == "Point" {
			foundPoint = true
		}
	}

	if !foundAdd || !foundPoint {
		t.Errorf("Expected 'Add' and 'Point' in completions, got: %+v", completions)
	}
}

func TestGlobalPackageCompletion(t *testing.T) {
	code := `package main
func main() {
	
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	externalSpecs := map[ast.Ident]ast.GoMiniType{
		"os.ReadFile": "function(String) (TypeBytes, String)",
		"fmt.Printf":  "function(String, ...Any) Void",
	}

	ast.NewValidator(prog, externalSpecs, nil, true)
	// 在空行触发补全
	completions := ast.FindCompletionsAt(prog, 3, 1)

	foundOs := false
	foundFmt := false
	for _, item := range completions {
		if item.Label == "os" && item.Kind == "package" {
			foundOs = true
		}
		if item.Label == "fmt" && item.Kind == "package" {
			foundFmt = true
		}
	}

	if !foundOs || !foundFmt {
		t.Errorf("Expected 'os' and 'fmt' packages in global completion list, got: %+v", completions)
	}
}

func TestUnimportedFFIValidationError(t *testing.T) {
	code := `package main
func main() {
	os.NonExistentFunction("test.txt")
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	// 模拟 FFI 符号，但不包含 NonExistentFunction
	externalSpecs := map[ast.Ident]ast.GoMiniType{
		"os.ReadFile": "function(String) (TypeBytes, String)",
	}

	validator, _ := ast.NewValidator(prog, externalSpecs, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	err = prog.Check(semanticCtx)

	// 预期有验证错误，因为 NonExistentFunction 不存在
	if err == nil {
		t.Errorf("Expected validation error for non-existent function in unimported FFI package, but got none")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

func TestUnimportedUnknownPackageCompletion(t *testing.T) {
	code := `package main
func main() {
	unknownpkg.SomeFunc()
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	externalSpecs := map[ast.Ident]ast.GoMiniType{
		"os.ReadFile": "function(String) (TypeBytes, String)",
	}

	ast.NewValidator(prog, externalSpecs, nil, true)
	// 在 "unknownpkg." 之后触发补全 (Line 3, Col 12 是 '.')
	completions := ast.FindCompletionsAt(prog, 3, 12)

	if len(completions) > 0 {
		t.Errorf("Expected no completions for unknown package, but got: %+v", completions)
	}
}

func TestUnimportedUnknownPackageCheck(t *testing.T) {
	code := `package main
func main() {
	unknownpkg.SomeFunc()
}`
	conv := ffigo.NewGoToASTConverter()
	node, err := conv.ConvertSource("snippet", code)
	if err != nil {
		t.Fatal(err)
	}
	prog := node.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(prog, nil, nil, true)
	semanticCtx := ast.NewSemanticContext(validator)
	err = prog.Check(semanticCtx)

	// 预期有验证错误，因为 unknownpkg 既没导入也不是 FFI 包
	if err == nil {
		t.Errorf("Expected validation error for unknown package, but got none")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

func TestImportedRootCompletionWithoutExplicitImport(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	// 模拟已加载但未导入的子模块
	subCode := `package mymath
func Add(a Int64, b Int64) Int64 { return a + b }`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)

	mainCode := `package main
func main() {
	mymath.Add(1, 2)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(mainProg, nil, nil, true)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	// 模拟 Loader 已加载了该包，但 main 代码中没有 import "my/math"
	validator.Root().ImportedRoots["mymath"] = subValidator.Root()

	// 在 "mymath." 之后触发补全 (Line 3, Col 8 是 '.')
	completions := ast.FindCompletionsAt(mainProg, 3, 8)

	foundAdd := false
	for _, item := range completions {
		if item.Label == "Add" {
			foundAdd = true
		}
	}

	if !foundAdd {
		t.Errorf("Expected 'Add' in completions for unimported but loaded package, but got: %+v", completions)
	}
}

func TestImportedRootCheckWithoutExplicitImport(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	// 模拟已加载但未导入的子模块
	subCode := `package mymath
func Add(a Int64, b Int64) Int64 { return a + b }`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)

	mainCode := `package main
func main() {
	mymath.Add(1, 2)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(mainProg, nil, nil, true)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	// 模拟 Loader 已加载了该包，但 main 代码中没有 import "my/math"
	validator.Root().ImportedRoots["mymath"] = subValidator.Root()

	semanticCtx := ast.NewSemanticContext(validator)
	err = mainProg.Check(semanticCtx)

	// 预期有验证错误，因为没导入，即使 Loader 已经把它读进来了
	if err == nil {
		t.Errorf("Expected validation error for unimported package, but got none")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

func TestImportedRootPathCompletionWithoutExplicitImport(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	// 模拟已加载但未导入的子模块，路径带有层级
	subCode := `package math
func Add(a Int64, b Int64) Int64 { return a + b }`
	subNode, err := conv.ConvertSource("math", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)

	mainCode := `package main
func main() {
	math.Add(1, 2)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(mainProg, nil, nil, true)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	// 模拟 Loader 已加载了该包，路径为 "my/math"
	validator.Root().ImportedRoots["my/math"] = subValidator.Root()

	// 重新初始化以应用宽容模式下的 Package 注册逻辑 (实际上通常是在 Loader 填充后再创建 Validator，这里手动触发)
	validator, _ = ast.NewValidator(mainProg, nil, nil, true)
	validator.Root().ImportedRoots["my/math"] = subValidator.Root()
	// 重新运行部分逻辑或手动添加，因为 NewValidator 在创建时就已经遍历了 ImportedRoots（此时还为空）
	// 在实际 LSP 中，Validator 可能是持久的或在 Loader 填充后创建的。
	// 让我们模拟 LSP 中常见的：填充 ImportedRoots 之后的情况。
	validator.Root().Vars()["math"] = "Package"

	// 在 "math." 之后触发补全 (Line 3, Col 6 是 '.')
	completions := ast.FindCompletionsAt(mainProg, 3, 6)

	foundAdd := false
	for _, item := range completions {
		if item.Label == "Add" {
			foundAdd = true
		}
	}

	if !foundAdd {
		t.Errorf("Expected 'Add' in completions for 'my/math' package accessed as 'math', but got: %+v", completions)
	}
}

func TestImportedRootCheckWithoutExplicitImportButWithPackageType(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	// 模拟已加载但未导入的子模块
	subCode := `package mymath
func Add(a Int64, b Int64) Int64 { return a + b }`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)

	mainCode := `package main
func main() {
	mymath.Add(1, 2)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(mainProg, nil, nil, true)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	validator.Root().ImportedRoots["mymath"] = subValidator.Root()
	// 手动注册 Package 类型，模拟宽容模式下的发现
	validator.Root().Vars()["mymath"] = "Package"

	semanticCtx := ast.NewSemanticContext(validator)
	err = mainProg.Check(semanticCtx)
	if err == nil {
		t.Fatalf("Expected validation error for recognized but unimported package, but got none")
	}

	foundMissingImport := false
	for _, log := range validator.Logs() {
		if strings.Contains(log.Message, "已解析但未导入") {
			foundMissingImport = true
			break
		}
	}
	if !foundMissingImport {
		t.Fatalf("Expected missing import diagnostic, got logs: %+v", validator.Logs())
	}
}

func TestImportedRootCheckMemberNotExist(t *testing.T) {
	conv := ffigo.NewGoToASTConverter()

	subCode := `package mymath
func Add(a Int64, b Int64) Int64 { return a + b }`
	subNode, err := conv.ConvertSource("mymath", subCode)
	if err != nil {
		t.Fatal(err)
	}
	subProg := subNode.(*ast.ProgramStmt)

	mainCode := `package main
func main() {
	mymath.Sub(1, 2)
}`
	mainNode, err := conv.ConvertSource("main", mainCode)
	if err != nil {
		t.Fatal(err)
	}
	mainProg := mainNode.(*ast.ProgramStmt)

	validator, _ := ast.NewValidator(mainProg, nil, nil, true)
	subValidator, _ := ast.NewValidator(subProg, nil, nil, true)
	_ = subProg.Check(ast.NewSemanticContext(subValidator))

	validator.Root().ImportedRoots["mymath"] = subValidator.Root()
	validator.Root().Vars()["mymath"] = "Package"

	semanticCtx := ast.NewSemanticContext(validator)
	err = mainProg.Check(semanticCtx)

	// 预期有验证错误，因为 Sub 在 mymath 中不存在
	if err == nil {
		t.Errorf("Expected validation error for non-existent member in recognized package, but got none")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

func TestCompletionDoesNotLeakFutureLocalVariables(t *testing.T) {
	code := `package main
func main() {
	prin
	later := 1
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

	completions := ast.FindCompletionsAt(prog, 3, 5)
	for _, item := range completions {
		if item.Label == "later" {
			t.Fatalf("future local variable leaked into completion list: %+v", completions)
		}
	}
}
