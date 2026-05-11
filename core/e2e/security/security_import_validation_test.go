package tests

import (
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestPathTraversalSecurity(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		return nil, nil
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

func TestImportDepthLimit(t *testing.T) {
	executor := engine.NewMiniExecutor()

	depth := 0
	executor.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
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
