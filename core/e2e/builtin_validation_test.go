package e2e

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestInvalidMakeCompileError(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func main() {
		m := make("InvalidType")
	}
	`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil || !strings.Contains(err.Error(), "非法类型") {
		t.Fatalf("Expected compile error for invalid make type, got: %v", err)
	}
}

func TestNewBuiltinCompileError(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func main() {
		p := new("InvalidType")
	}
	`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil || !strings.Contains(err.Error(), "非法类型") {
		t.Fatalf("Expected compile error for invalid new type, got: %v", err)
	}
}
