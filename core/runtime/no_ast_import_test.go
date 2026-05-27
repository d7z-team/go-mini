package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimeNonTestFilesDoNotImportFrontendOrDebugger(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read runtime dir failed: %v", err)
	}
	forbidden := []struct {
		importPath string
		name       string
	}{
		{importPath: `"gopkg.d7z.net/go-mini/core/ast"`, name: "core/ast"},
		{importPath: `"gopkg.d7z.net/go-mini/core/debugger"`, name: "core/debugger"},
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
		source := string(data)
		for _, rule := range forbidden {
			if strings.Contains(source, rule.importPath) {
				t.Fatalf("runtime non-test file %s imports %s", name, rule.name)
			}
		}
	}
}
