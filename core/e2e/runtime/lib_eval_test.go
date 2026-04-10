package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestLibEval(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 1. 加载一个只有函数定义的“库脚本”
	libCode := `
package mathlib

func Factorial(n int) int {
	if n <= 1 { return 1 }
	return n * Factorial(n-1)
}

func Add(a, b int) int {
	return a + b
}
`
	prog, err := executor.NewRuntimeByGoCode(libCode)
	if err != nil {
		t.Fatalf("Failed to compile lib: %v", err)
	}

	// 2. 即使不运行 Execute()，函数定义也已经在蓝图中了
	// 我们通过 Eval 直接调用这些函数

	t.Run("Call Add", func(t *testing.T) {
		env := map[string]interface{}{
			"x": int64(10),
			"y": int64(20),
		}
		results, err := prog.Eval(context.Background(), "Add(x, y)", env)
		if err != nil {
			t.Fatalf("Eval Add failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Eval returned %d values, want 1", len(results))
		}
		res := results[0]
		if res.I64 != 30 {
			t.Errorf("Expected 30, got %d", res.I64)
		}
	})

	t.Run("Call Factorial", func(t *testing.T) {
		results, err := prog.Eval(context.Background(), "Factorial(5)", nil)
		if err != nil {
			t.Fatalf("Eval Factorial failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Eval returned %d values, want 1", len(results))
		}
		res := results[0]
		if res.I64 != 120 {
			t.Errorf("Expected 120, got %d", res.I64)
		}
	})
}

func TestCrossPackageEval(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 1. 注册一个“库”
	libCode := `
package lib
func Hello(name string) string {
	return "Hello, " + name
}
`
	prog, _ := executor.NewRuntimeByGoCode(libCode)
	executor.RegisterModule("mylib", prog)

	// 3. 在 Eval 中进行跨包调用 (通过 Import 注入)
	t.Run("Import and Eval", func(t *testing.T) {
		// 预加载模块
		lib, err := executor.Import(context.Background(), "mylib")
		if err != nil {
			t.Fatalf("Import mylib failed: %v", err)
		}

		// 注入到 Eval 环境
		env := map[string]interface{}{
			"lib": lib,
		}

		results, err := executor.Eval(context.Background(), "lib.Hello(\"Mini\")", env)
		if err != nil {
			t.Fatalf("Eval with lib failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Eval returned %d values, want 1", len(results))
		}
		res := results[0]

		if res.Str != "Hello, Mini" {
			t.Errorf("Expected 'Hello, Mini', got %q", res.Str)
		}
	})
}
