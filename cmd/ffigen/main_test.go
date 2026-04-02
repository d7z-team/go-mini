package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDirectoryModeGeneratesPackageOutputAndSkipsOwnedStructRegistrations(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "page.go", `package pkgmode

// ffigen:methods Page
type Page struct {
	TitleText string
}

func (p *Page) Title() string {
	return p.TitleText
}
`)
	writeTestFile(t, workspace, "browser.go", `package pkgmode

// ffigen:module browser
type BrowserModule interface {
	Open() *Page
}
`)

	outputDir := filepath.Join(workspace, "gen")
	oldPkg, oldOut := *pkgName, *outFile
	*pkgName = "pkgmode"
	*outFile = outputDir
	t.Cleanup(func() {
		*pkgName = oldPkg
		*outFile = oldOut
	})

	if err := runDirectoryMode(workspace); err != nil {
		t.Fatalf("runDirectoryMode: %v", err)
	}

	generatedPath := filepath.Join(outputDir, "ffigen_pkgmode.go")
	content, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read generated output: %v", err)
	}
	code := string(content)

	if strings.Count(code, "var browser_Page_FFI_StructSchema = ") != 1 {
		t.Fatalf("expected browser.Page schema to be emitted once, got %d", strings.Count(code, "var browser_Page_FFI_StructSchema = "))
	}
	if strings.Count(code, `registerStructSchema("browser.Page",`) != 1 {
		t.Fatalf("expected browser.Page to be registered once by its owned target, got %d", strings.Count(code, `registerStructSchema("browser.Page",`))
	}
	if !strings.Contains(code, "func RegisterBrowserModule(") {
		t.Fatalf("expected module target to be generated")
	}
	if !strings.Contains(code, "func RegisterPage(") {
		t.Fatalf("expected owned struct target to be generated")
	}
}

func TestRunDirectoryModeRejectsMultipleModules(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "a.go", `package pkgmode

// ffigen:module a
type A interface {
	Ping() int64
}
`)
	writeTestFile(t, workspace, "b.go", `package pkgmode

// ffigen:module b
type B interface {
	Pong() int64
}
`)

	oldPkg, oldOut := *pkgName, *outFile
	*pkgName = "pkgmode"
	*outFile = filepath.Join(workspace, "gen")
	t.Cleanup(func() {
		*pkgName = oldPkg
		*outFile = oldOut
	})

	err := runDirectoryMode(workspace)
	if err == nil || !strings.Contains(err.Error(), "at most one ffigen:module") {
		t.Fatalf("expected multiple-module error, got %v", err)
	}
}

func TestRunFileModeRejectsReservedPackageOutputName(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "api.go", `package pkgmode

type Demo interface {
	Ping() int64
}
`)

	oldPkg, oldOut := *pkgName, *outFile
	*pkgName = "pkgmode"
	*outFile = filepath.Join(workspace, "ffigen_pkgmode.go")
	t.Cleanup(func() {
		*pkgName = oldPkg
		*outFile = oldOut
	})

	err := runFileMode([]string{filepath.Join(workspace, "api.go")})
	if err == nil || !strings.Contains(err.Error(), "reserved package output name") {
		t.Fatalf("expected reserved-name rejection, got %v", err)
	}
}

func makeModuleTempDir(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	workspace, err := os.MkdirTemp(cwd, "ffigen-main-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(workspace)
	})
	return workspace
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
