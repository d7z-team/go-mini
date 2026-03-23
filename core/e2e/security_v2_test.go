package e2e

import (
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// TestPathTraversalSecurity 路径穿越安全漏洞修复测试
func TestPathTraversalSecurity(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 设置一个简单的 Loader
	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		return nil, nil // 路径校验应该在此之前发生
	})

	testCases := []string{
		"../etc/passwd",
		"/etc/passwd",
		"lib/../../etc/passwd",
		"  ../test  ",
	}

	for _, tc := range testCases {
		code := "package main; import \"" + tc + "\"; func main() {}"
		_, err := executor.NewRuntimeByGoCode(code)
		if err == nil {
			t.Errorf("Path traversal allowed: %s", tc)
		} else if !strings.Contains(err.Error(), "invalid import path") {
			t.Errorf("Unexpected error for path %s: %v", tc, err)
		}
	}
}

// TestImportDepthLimit 导入深度限制测试
func TestImportDepthLimit(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 构造一个链式导入 A -> B -> C ...
	// 我们这里通过模拟 Loader 返回下一个模块来实现
	depth := 0
	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		depth++
		next := fmt.Sprintf("m%d", depth)
		code := fmt.Sprintf("package %s; import \"%s\"; func Run() {}", path, next)
		converter := ffigo.NewGoToASTConverter()
		node, _ := converter.ConvertSource("snippet", code)
		return node.(*ast.ProgramStmt), nil
	})

	code := "package main; import \"m0\"; func main() {}"
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("Should fail due to depth limit")
	}
	if !strings.Contains(err.Error(), "import depth limit exceeded") {
		t.Fatalf("Expected depth limit error, got: %v", err)
	}
}
