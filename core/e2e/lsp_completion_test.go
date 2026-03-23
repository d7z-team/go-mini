package e2e

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestLSPCompletion(t *testing.T) {
	e := engine.NewMiniExecutor()

	// 注入一个模拟的 FFI 函数签名
	e.AddFuncSpec("os.ReadFile", ast.GoMiniType("function(String) (String, Error)"))

	// 使用空格缩进，避免 Tab 导致的列号偏移不确定性
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
	// 使用容错模式
	prog, _ := e.NewMiniProgramByGoCodeTolerant(code)
	if prog == nil {
		t.Fatal("failed to get program")
	}

	// 1. 基础补全测试 (main 函数内)
	t.Run("BasicScope", func(t *testing.T) {
		// 在 os.ReadFile 这一行 (13) 的起始位置获取补全
		completions := prog.GetCompletionsAt(13, 1)

		foundKeywords := false
		foundBuiltins := false
		foundLocal := false
		foundOS := false

		for _, item := range completions {
			if item.Label == "if" && item.Kind == "keyword" {
				foundKeywords = true
			}
			if item.Label == "len" && item.Kind == "builtin" {
				foundBuiltins = true
			}
			if item.Label == "localMsg" && item.Kind == "var" {
				foundLocal = true
			}
			if item.Label == "os" && item.Kind == "package" {
				foundOS = true
			}
		}

		if !foundKeywords {
			t.Error("missing keywords")
		}
		if !foundBuiltins {
			t.Error("missing builtins")
		}
		if !foundLocal {
			t.Error("missing localMsg")
		}
		if !foundOS {
			t.Error("missing os package")
		}
	})

	// 2. 包成员补全测试 (os.)
	t.Run("PackageMember", func(t *testing.T) {
		// 光标在 os.ReadFile 的点号之后 (13, 8)
		completions := prog.GetCompletionsAt(13, 8)
		foundReadFile := false
		for _, item := range completions {
			if item.Label == "ReadFile" {
				foundReadFile = true
			}
		}
		if !foundReadFile {
			t.Errorf("expected to find ReadFile in os. completions, found %d items", len(completions))
			for _, it := range completions {
				t.Logf("- %s (%s)", it.Label, it.Kind)
			}
		}
	})

	// 3. 结构体成员补全测试 (obj.)
	t.Run("StructMember", func(t *testing.T) {
		// 光标在 obj.Field1 的点号之后 (12, 9)
		completions := prog.GetCompletionsAt(12, 9)
		foundField := false
		foundMethod := false
		for _, item := range completions {
			if item.Label == "Field1" && item.Kind == "field" {
				foundField = true
			}
			if item.Label == "Method1" && item.Kind == "method" {
				foundMethod = true
			}
		}
		if !foundField {
			t.Error("missing Field1")
		}
		if !foundMethod {
			t.Error("missing Method1")
		}
	})
}
