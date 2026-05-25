package e2e_test

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// TestLSPEnhancedCasing 验证成员补全的大小写是否正确 (Go 规范)
func TestLSPEnhancedCasing(t *testing.T) {
	testExecutor := newStdExecutor()

	sourceSnippet := `package main
import "fmt"
func main() {
    fmt.
}`
	// 使用容错解析，因为 fmt. 语法是不完整的
	testProgram, _ := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
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
	testExecutor := newStdExecutor()

	sourceSnippet := `package main
func main() {
	
}`
	testProgram, _ := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
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
	testExecutor := newStdExecutor()

	sourceSnippet := `package main
func main() {
  var a = 100
  printf("%s","asas")
}`
	_, err := testExecutor.NewRuntimeByGoCode(sourceSnippet)
	if err == nil {
		t.Fatal("expected error for undefined global printf, but got none")
	}

	expectedMsg := "printf"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("error message mismatch.\ngot: %v\nwant: %s", err, expectedMsg)
	}
}

func TestTolerantBuildDoesNotReportRuntimeBytecodeNoise(t *testing.T) {
	testExecutor := newStdExecutor()

	sourceSnippet := `package main
import "fmt"

func main() {
	fmt.Println("Hello")
	time.Sleep(1 * time.Second)
}`

	_, errs := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
	if len(errs) == 0 {
		t.Fatal("expected semantic diagnostics for missing import")
	}

	for _, err := range errs {
		if strings.Contains(err.Error(), "executable bytecode") {
			t.Fatalf("unexpected runtime noise in tolerant diagnostics: %v", err)
		}
	}
}

func TestTolerantBuildReportsUnknownImport(t *testing.T) {
	testExecutor := newStdExecutor()

	sourceSnippet := `package main
import "definitely/not-found"

func main() {}
`

	_, errs := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
	if len(errs) == 0 {
		t.Fatal("expected unknown import diagnostic")
	}

	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "module not found: definitely/not-found") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown import error, got: %+v", errs)
	}
}

func TestTolerantBuildAllowsRegisteredFFIImport(t *testing.T) {
	testExecutor := newStdExecutor()

	sourceSnippet := `package main
import "time"

func main() {
	time.Sleep(1 * time.Second)
}
`

	_, errs := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
	for _, err := range errs {
		if strings.Contains(err.Error(), "module not found: time") {
			t.Fatalf("registered FFI import should be accepted, got: %v", err)
		}
	}
}

func TestLSPTemplateHoverShowsFacadeAsImportStyle(t *testing.T) {
	testExecutor := newStdExecutor()
	if err := testExecutor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "audit.Log",
		PackagePath: "audit",
		Name:        "Log",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
		Body:        `{{ pkg "fmt" }}.Println({{ args }})`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	sourceSnippet := `package main
import a "audit"

func main() {
	a.Log("created", 1001)
}`

	testProgram, errs := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
	if len(errs) > 0 {
		t.Fatalf("unexpected diagnostics: %v", errs)
	}

	hover := hoverMarkdownAtSubstring(t, testProgram, sourceSnippet, "a.Log")
	for _, want := range []string{
		"template audit.Log",
		"import \"fmt\"",
		`fmt.Println("created", 1001)`,
	} {
		if !strings.Contains(hover, want) {
			t.Fatalf("hover markdown missing %q:\n%s", want, hover)
		}
	}
	for _, forbidden := range []string{"__gomini_tpl_", "pkg_fmt", "<arg"} {
		if strings.Contains(hover, forbidden) {
			t.Fatalf("hover markdown should not expose %q:\n%s", forbidden, hover)
		}
	}
}

func TestLSPTemplateHoverExpandsNestedTemplatesToFinalRender(t *testing.T) {
	testExecutor := newStdExecutor()
	if err := testExecutor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "trace.value",
		Name:      "traceValue",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
		Body: `println("first", {{ args }})
println("second", {{ args }})`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	sourceSnippet := `package main
func main() {
	count := 7
	traceValue(count+1)
}`

	testProgram, errs := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
	if len(errs) > 0 {
		t.Fatalf("unexpected diagnostics: %v", errs)
	}

	hover := hoverMarkdownAtSubstring(t, testProgram, sourceSnippet, "traceValue")
	for _, want := range []string{
		"template trace.value",
		`fmt.Println("first", count+1)`,
		`fmt.Println("second", count+1)`,
	} {
		if !strings.Contains(hover, want) {
			t.Fatalf("hover markdown missing %q:\n%s", want, hover)
		}
	}
	if strings.Contains(hover, "println(") || strings.Contains(hover, "__gomini_tpl_") {
		t.Fatalf("hover markdown should show final display render only:\n%s", hover)
	}
}

func hoverMarkdownAtSubstring(t *testing.T, program *engine.AnalysisProgram, source, needle string) string {
	t.Helper()
	index := strings.Index(source, needle)
	if index < 0 {
		t.Fatalf("source does not contain %q", needle)
	}
	prefix := source[:index]
	line := strings.Count(prefix, "\n") + 1
	lastNewline := strings.LastIndex(prefix, "\n")
	col := index + 1
	if lastNewline >= 0 {
		col = index - lastNewline
	}
	hover := program.GetHoverAt(line, col)
	if hover == nil || hover.Markdown == "" {
		t.Fatalf("expected template hover at %d:%d for %q, got %#v", line, col, needle, hover)
	}
	return hover.Markdown
}
