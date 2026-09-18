package ffigo

import (
	"os"
	"strings"
	"testing"
)

func TestFFIGoPackageDoesNotImportFrontendOrMiniAST(t *testing.T) {
	forbidden := []string{
		`"gopkg.d7z.net/go-mini/core/ast"`,
		`"go/ast"`,
		`"go/parser"`,
		`"go/scanner"`,
		`"go/token"`,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read ffigo dir failed: %v", err)
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
		content := string(data)
		for _, item := range forbidden {
			if strings.Contains(content, item) {
				t.Fatalf("ffigo non-test file %s imports frontend dependency %s", name, item)
			}
		}
	}
}
