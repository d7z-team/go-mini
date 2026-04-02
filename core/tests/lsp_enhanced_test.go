package engine_test

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

// TestLSPEnhancedCasing 验证成员补全的大小写是否正确 (Go 规范)
func TestLSPEnhancedCasing(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	testExecutor.InjectStandardLibraries()

	sourceSnippet := `package main
import "fmt"
func main() {
    fmt.
}`
	// 使用容错解析，因为 fmt. 语法是不完整的
	testProgram, _ := testExecutor.NewMiniProgramByGoCodeTolerant(sourceSnippet)
	if testProgram == nil {
		t.Fatal("failed to get program even in tolerant mode")
	}

	// 尝试在 fmt. 后面获取补全 (第4行, 第9列，即点号之后)
	completionItems := testProgram.GetCompletionsAt(4, 9)

	foundPrintf := false
	var foundLabels []string
	for _, item := range completionItems {
		foundLabels = append(foundLabels, item.Label)
		if item.Label == "Printf" {
			foundPrintf = true
		}
		if item.Label == "printf" {
			t.Errorf("found lowercase 'printf' in fmt members, expected 'Printf'")
		}
	}

	if !foundPrintf {
		t.Errorf("Printf not found in fmt members. Found: %s", strings.Join(foundLabels, ", "))
	}
}

// TestLSPGlobalBuiltins 验证全局内置函数是否去重且大小写正确
func TestLSPGlobalBuiltins(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	testExecutor.InjectStandardLibraries()

	sourceSnippet := `package main
func main() {
	
}`
	testProgram, _ := testExecutor.NewMiniProgramByGoCodeTolerant(sourceSnippet)
	completionItems := testProgram.GetCompletionsAt(3, 1)

	printCount := 0
	printlnCount := 0
	for _, item := range completionItems {
		if item.Label == "print" {
			printCount++
		}
		if item.Label == "println" {
			printlnCount++
		}
		if item.Label == "Printf" || item.Label == "printf" {
			t.Errorf("global scope should not contain printf/Printf without package prefix")
		}
	}

	if printCount != 1 {
		t.Errorf("expected exactly 1 'print' in global scope, got %d", printCount)
	}
	if printlnCount != 1 {
		t.Errorf("expected exactly 1 'println' in global scope, got %d", printlnCount)
	}
}

// TestLSPUserScenarioRegression 验证用户报告的 printf 不存在错误
func TestLSPUserScenarioRegression(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	testExecutor.InjectStandardLibraries()

	sourceSnippet := `package main
func main() {
  var a = 100
  printf("%s","asas")
}`
	_, err := testExecutor.NewRuntimeByGoCode(sourceSnippet)
	if err == nil {
		t.Fatal("expected error for undefined global printf, but got none")
	}

	expectedMsg := "printf 不存在"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("error message mismatch.\ngot: %v\nwant: %s", err, expectedMsg)
	}
}
