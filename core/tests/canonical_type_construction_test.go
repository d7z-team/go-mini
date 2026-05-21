package engine_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalTypesAreBuiltThroughTypeHelpers(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	forbidden := regexp.MustCompile(`"((Ptr|HostRef|Array|Map)<|tuple\(|function\(|interface\{)"\s*\+|fmt\.Sprintf\("((Ptr|HostRef|Array|Map)<|tuple\(|function\(|interface\{)`)
	var violations []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "core"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if canonicalTypeConstructionAllowed(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if forbidden.Match(data) {
			violations = append(violations, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("manual canonical type construction outside allowed helpers:\n%s", strings.Join(violations, "\n"))
	}
}

func canonicalTypeConstructionAllowed(rel string) bool {
	if rel == filepath.ToSlash(filepath.Join("core", "typespec", "typespec.go")) {
		return true
	}
	if rel == filepath.ToSlash(filepath.Join("core", "ast", "ast_types.go")) {
		return true
	}
	base := filepath.Base(rel)
	return strings.HasSuffix(base, "_ffigen.go") || strings.HasSuffix(base, "_test.go")
}
