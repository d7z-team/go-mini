package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimeNonTestFilesDoNotImportMiniAST(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read runtime dir failed: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s failed: %v", name, err)
		}
		if strings.Contains(string(data), `"gopkg.d7z.net/go-mini/core/ast"`) {
			t.Fatalf("runtime non-test file %s imports core/ast", name)
		}
	}
}
