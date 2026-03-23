package e2e

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	engine "gopkg.d7z.net/go-mini/core"
)

func TestLSPCompletion(t *testing.T) {
	e := engine.NewMiniExecutor()
	
	// 注入一个模拟的 FFI 函数签名
	e.AddFuncSpec("os.ReadFile", ast.GoMiniType("function(String) (String, Error)"))

	code := `package main

type MyStruct struct {
	Field1 Int64
}

func (m *MyStruct) Method1() {}

func main() {
	var localMsg = "hello"
	var obj MyStruct
	obj.Field1 = 1
	os.ReadFile("test.txt")
}`
	prog, err := e.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	// 1. 基础补全测试 (main 函数内)
	t.Run("BasicScope", func(t *testing.T) {
		completions := prog.GetCompletionsAt(13, 2) // `\tos...` 的 'o' 上
		foundKeywords := false
		foundBuiltins := false
		foundLocal := false
		foundOS := false

		for _, item := range completions {
			if item.Label == "if" && item.Kind == "keyword" { foundKeywords = true }
			if item.Label == "len" && item.Kind == "builtin" { foundBuiltins = true }
			if item.Label == "localMsg" { foundLocal = true }
			if item.Label == "os" && item.Kind == "package" { foundOS = true }
		}

		if !foundKeywords { t.Error("missing keywords") }
		if !foundBuiltins { t.Error("missing builtins") }
		if !foundLocal { t.Error("missing localMsg") }
		if !foundOS { t.Error("missing os package") }
	})

	// 2. 包成员补全测试 (os.)
	t.Run("PackageMember", func(t *testing.T) {
		// os.ReadFile 在第 13 行。os 后面是点号 (第4列为 '.', 第5列为 'R')
		completions := prog.GetCompletionsAt(13, 4)
		foundReadFile := false
		for _, item := range completions {
			if item.Label == "ReadFile" {
				foundReadFile = true
			}
		}
		if !foundReadFile {
			t.Errorf("expected to find ReadFile in os. completions, found %d items", len(completions))
			for _, it := range completions { t.Logf("- %s (%s)", it.Label, it.Kind) }
		}
	})

	// 3. 结构体成员补全测试 (obj.)
	t.Run("StructMember", func(t *testing.T) {
		// obj.Field1 在第 12 行。点号在第4列 (如果有个tab的话，tab算1个字符长度)
		// '\t' -> col 1, 'o' -> 2, 'b' -> 3, 'j' -> 4, '.' -> 5
		completions := prog.GetCompletionsAt(12, 5)
		foundField := false
		foundMethod := false
		for _, item := range completions {
			if item.Label == "Field1" && item.Kind == "field" { foundField = true }
			if item.Label == "Method1" && item.Kind == "method" { foundMethod = true }
		}
		if !foundField { t.Error("missing Field1") }
		if !foundMethod { t.Error("missing Method1") }
	})
}