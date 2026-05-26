package tests

import (
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestPathTraversalSecurity(t *testing.T) {
	executor := engine.NewMiniExecutor()

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

	modules := make([]surface.LibraryModule, 0, 111)
	for i := 0; i <= 110; i++ {
		path := fmt.Sprintf("m%d", i)
		code := fmt.Sprintf("package %s; func Run() {}", path)
		if i < 110 {
			next := fmt.Sprintf("m%d", i+1)
			code = fmt.Sprintf("package %s; import %q; func Run() {}", path, next)
		}
		modules = append(modules, surface.LibraryModule{
			Path:  path,
			Files: []surface.LibraryFile{surface.GoFile(path+".mgo", code)},
		})
	}
	if err := executor.UseSurface(surface.Libraries(modules...)); err != nil {
		t.Fatalf("register deep import libraries: %v", err)
	}

	code := "package main; import \"m0\"; func main() {}"
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("Should fail due to depth limit")
	}
	if !strings.Contains(err.Error(), "import depth limit exceeded") {
		t.Fatalf("Expected depth limit error, got: %v", err)
	}
}
