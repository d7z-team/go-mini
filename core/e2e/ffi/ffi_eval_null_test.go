package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

// TestFFIEvalQuerySelectorNull 复现 1.mgo 中的 panic 场景：
//
// line 193: page.Eval(`document.querySelector('.down').click()`)
//
// 根因分析：
// document.querySelector('.down') 在 DOM 中找不到对应元素时返回 null，
// 随后对 null 调用 .click() 抛出 TypeError: Cannot read properties of null (reading 'click')。
// 该错误从浏览器 CDP 层传递到 FFI Bridge，Bridge 将其作为 panic 抛出，
// executor_ffi.go:89 的 defer/recover 捕获后包装为 VMError{IsPanic: true}，
// 最后在 Mini 脚本中表现为未捕获的 panic。
func TestFFIEvalQuerySelectorNull(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	bridge := &querySelectorNullBridge{}
	testsurface.UseRoute(t, executor, "page.Eval", bridge, 0, runtime.MustParseRuntimeFuncSig("function(String) Any"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "page"

func main() {
	page.Eval(` + "`" + `document.querySelector('.down').click()` + "`" + `)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected unhandled FFI panic to fail execution")
	}
	if !strings.Contains(err.Error(), "TypeError: Cannot read properties of null (reading 'click')") {
		t.Fatalf("expected querySelector null TypeError, got: %v", err)
	}
}

// TestFFIEvalQuerySelectorNullRecovered 验证 Mini 脚本可以通过 defer/recover
// 捕获 page.Eval() 的 FFI panic，避免脚本崩溃。
func TestFFIEvalQuerySelectorNullRecovered(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	bridge := &querySelectorNullBridge{}
	testsurface.UseRoute(t, executor, "page.Eval", bridge, 0, runtime.MustParseRuntimeFuncSig("function(String) Any"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "page"

func main() {
	defer func() {
		r := recover()
		if r == nil {
			panic("expected recover value")
		}
	}()

	page.Eval("document.querySelector('.down').click()")
	panic("unreachable")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
