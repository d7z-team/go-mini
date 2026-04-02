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
	if !strings.Contains(code, `registrar.RegisterFFISchema("browser.Page.Title"`) {
		t.Fatalf("expected owned struct methods to use dotted method routes")
	}
}

func TestRunDirectoryModeDoesNotTreatVariadicArgumentAsStructReceiver(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "selector.go", `package pkgmode

// ffigen:methods Locator
type Locator struct{}

func (l *Locator) Locator() *Locator {
	return l
}

// ffigen:module browser
type BrowserModule interface {
	All(selectors ...string) *Locator
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
	if strings.Contains(code, "....") {
		t.Fatalf("generated code contains invalid variadic member access:\n%s", code)
	}
	if !strings.Contains(code, "r0 := impl.All(selectors...)") {
		t.Fatalf("expected variadic module call to target impl directly")
	}
}

func TestRunDirectoryModeUsesInjectedReceiverForStructMethods(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "selector.go", `package pkgmode

// ffigen:methods CdpSelector
type CdpSelector struct{}

func (o *CdpSelector) DragTo(target *CdpSelector) {}
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
	if !strings.Contains(code, "__recv.DragTo(target)") {
		t.Fatalf("expected generated struct method call to use injected receiver, got:\n%s", code)
	}
	if strings.Contains(code, "target.DragTo()") {
		t.Fatalf("generated code still misuses target as receiver:\n%s", code)
	}
}

func TestRunDirectoryModePreservesGroupedStructMethodParametersInSchema(t *testing.T) {
	workspace := makeModuleTempDir(t)
	writeTestFile(t, workspace, "table.go", `package pkgmode

// ffigen:methods Table
type Table struct{}

func (t *Table) SetString(row, col int, val string) {}
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
	required := `SetString function(Ptr<Table>, Int64, Int64, String) Void;`
	if !strings.Contains(code, required) {
		t.Fatalf("expected grouped params to be preserved in struct schema, missing %q in:\n%s", required, code)
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

func TestDetectGenerationModeRejectsGeneratedFileInput(t *testing.T) {
	mode, err := detectGenerationMode([]string{"./", "ffigen_ops.go"})
	if err == nil || !strings.Contains(err.Error(), "generated file") {
		t.Fatalf("expected generated-file rejection, got mode=%v err=%v", mode, err)
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
