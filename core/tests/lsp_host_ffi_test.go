package engine_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestLSPHostFFICompletion(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	testExecutor.InjectStandardLibraries()
	// 1. 注册 FFI 结构体
	// 模拟 os.File
	testExecutor.DeclareStructSchema("os.File", runtime.MustParseRuntimeStructSpec("os.File", "struct { Read function(TypeBytes) tuple(Int64, Error); Close function() Error; Name String }"))

	// 模拟返回该结构体的函数
	testExecutor.DeclareFuncSchema("os.Open", runtime.MustParseRuntimeFuncSig("function(String) tuple(os.File, Error)"))

	sourceSnippet := `package main
import "os"

func main() {
    f, err := os.Open("test.txt")
    if err == nil {
        f.Read(make([]byte, 1024))
        f.Close()
        print(f.Name)
    }
}`

	testProgram, parseErrs := testExecutor.NewMiniProgramByGoCodeTolerant(sourceSnippet)
	if testProgram == nil {
		t.Fatal("failed to get program")
	}

	// 验证是否有语义错误 (排除语法错误)
	for _, parseErr := range parseErrs {
		if _, ok := parseErr.(*ast.MiniAstError); ok {
			t.Errorf("Unexpected semantic error: %v", parseErr)
		}
	}

	// 2. 验证包成员补全 (os.)
	t.Run("OS_PackageCompletion", func(t *testing.T) {
		// 光标在 os.Open 的点号之后 (5, 18)
		completionItems := testProgram.GetCompletionsAt(5, 18)
		foundOpen := false
		for _, item := range completionItems {
			if item.Label == "Open" && item.Kind == "func" {
				foundOpen = true
				break
			}
		}
		if !foundOpen {
			t.Error("missing os.Open completion")
			for _, item := range completionItems {
				t.Logf("- %s (%s)", item.Label, item.Kind)
			}
		}
	})

	// 3. 验证 FFI 结构体成员补全 (f.)
	t.Run("FFI_StructCompletion", func(t *testing.T) {
		// 光标在 f.Read 的点号之后 (7, 11)
		completionItems := testProgram.GetCompletionsAt(7, 11)
		foundRead := false
		foundClose := false
		foundName := false
		for _, item := range completionItems {
			if item.Label == "Read" && item.Kind == "method" {
				foundRead = true
			}
			if item.Label == "Close" && item.Kind == "method" {
				foundClose = true
			}
			if item.Label == "Name" && item.Kind == "field" {
				foundName = true
			}
		}
		if !foundRead {
			t.Error("missing f.Read completion")
		}
		if !foundClose {
			t.Error("missing f.Close completion")
		}
		if !foundName {
			t.Error("missing f.Name completion")
		}

		if !foundRead || !foundClose || !foundName {
			for _, item := range completionItems {
				t.Logf("- %s (%s) Type: %s", item.Label, item.Kind, item.Type)
			}
		}
	})
}
