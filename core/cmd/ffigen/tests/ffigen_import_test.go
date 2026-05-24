package tests

import (
	"os"
	"strings"
	"testing"
)

func TestFFIGenCrossPackageImport(t *testing.T) {
	content, err := os.ReadFile("importtest/ffigen.go")
	if err != nil {
		t.Fatalf("Failed to read generated FFI file: %v", err)
	}

	code := string(content)

	// 1. 验证导入块是否包含 "time"
	if !strings.Contains(code, "\"time\"") {
		t.Errorf("Generated code should contain import \"time\"\nFull code:\n%s", code)
	}

	// 2. 验证路由解码仍使用源 Go 类型 time.Duration
	requiredDurationCode := []string{
		"var d time.Duration",
		"d = time.Duration(tmp)",
		"err := impl.Sleep(ctx, d)",
	}
	for _, pattern := range requiredDurationCode {
		if !strings.Contains(code, pattern) {
			t.Errorf("Generated route should contain: %s", pattern)
		}
	}

	// 3. 验证是否包含默认导入
	requiredImports := []string{"context", "gopkg.d7z.net/go-mini/core/ffigo", "gopkg.d7z.net/go-mini/core/runtime"}
	for _, imp := range requiredImports {
		if !strings.Contains(code, imp) {
			t.Errorf("Missing required import: %s", imp)
		}
	}
}
