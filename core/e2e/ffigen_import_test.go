package e2e

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

	// 2. 验证方法签名是否正确使用了 time.Duration
	expectedSignature := "Sleep(ctx context.Context, d time.Duration) error"
	if !strings.Contains(code, expectedSignature) {
		t.Errorf("Generated method signature should contain: %s", expectedSignature)
	}

	// 3. 验证是否包含默认导入
	requiredImports := []string{"context", "gopkg.d7z.net/go-mini/core/ffigo", "gopkg.d7z.net/go-mini/core/ast"}
	for _, imp := range requiredImports {
		if !strings.Contains(code, imp) {
			t.Errorf("Missing required import: %s", imp)
		}
	}
}
