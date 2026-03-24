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

	// 4. new 关键字补全测试
	t.Run("NewCompletion", func(t *testing.T) {
		code2 := `package main
type Dat struct {
    data Int64
    value Int64
}
func main() {
    da := new(Dat)
    da.
}`
		prog2, _ := e.NewMiniProgramByGoCodeTolerant(code2)
		if prog2 == nil {
			t.Fatal("failed to get program")
		}
		completions := prog2.GetCompletionsAt(8, 8)
		foundData := false
		foundValue := false
		for _, item := range completions {
			if item.Label == "data" {
				foundData = true
			}
			if item.Label == "value" {
				foundValue = true
			}
		}
		if !foundData || !foundValue {
			t.Errorf("missing data or value in new(Dat) completions, found: %v", completions)
		}
	})

	// 5. make 关键字补全测试
	t.Run("MakeCompletion", func(t *testing.T) {
		code3 := `package main
func main() {
    da := make(map[string]int64)
    da.
}`
		prog3, _ := e.NewMiniProgramByGoCodeTolerant(code3)
		if prog3 == nil {
			t.Fatal("failed to get program")
		}
		// 验证类型是否正确推导
		node := prog3.GetNodeAt(4, 5) // "da"
		if node == nil {
			t.Fatal("da node not found")
		}
		if node.GetBase().Type != "Map<String, Int64>" {
			t.Errorf("Expected type Map<String, Int64>, got %s", node.GetBase().Type)
		}
	})

	// 6. append 关键字类型推导测试
	t.Run("AppendInference", func(t *testing.T) {
		code4 := `package main
func main() {
    list := make([]int64, 0)
    list2 := append(list, 1)
    list2.
}`
		prog4, _ := e.NewMiniProgramByGoCodeTolerant(code4)
		if prog4 == nil {
			t.Fatal("failed to get program")
		}
		node := prog4.GetNodeAt(4, 5) // "list2"
		if node == nil {
			t.Fatal("list2 node not found")
		}
		if node.GetBase().Type != "Array<Int64>" {
			t.Errorf("Expected type Array<Int64>, got %s", node.GetBase().Type)
		}
	})

	// 7. len 关键字类型推导测试
	t.Run("LenInference", func(t *testing.T) {
		code5 := `package main
func main() {
    l := len("hello")
    l.
}`
		prog5, _ := e.NewMiniProgramByGoCodeTolerant(code5)
		if prog5 == nil {
			t.Fatal("failed to get program")
		}
		// 获取 main 函数
		mainFunc := prog5.Program.Functions["main"]
		if mainFunc == nil {
			t.Fatal("main function not found")
		}
		// 获取 main 函数体的作用域
		scope, ok := mainFunc.Body.GetBase().Scope.(*ast.ValidContext)
		if !ok || scope == nil {
			t.Fatal("main body scope not found or invalid type")
		}
		node, ok := scope.GetVariable("l")
		if !ok || node != "Int64" {
			t.Errorf("Expected type Int64 for 'l', got %v", node)
		}
	})
}
