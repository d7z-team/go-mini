package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/surface"
)

func TestExportMetadataIncludesBuiltinsFFIAndSourceLibraries(t *testing.T) {
	testExecutor := newStdExecutor()

	sourceProgram := `package main
const VERSION = "1.0.0"
var Total = 7
type Count int
type Point struct { X, Y int }
func Add(a, b int) int { return a + b }
func (p Point) Distance() int { return 0 }
func main() {}`

	if err := testExecutor.UseSurface(surface.Library("main", surface.GoFile("main.mgo", sourceProgram))); err != nil {
		t.Fatal(err)
	}

	metadataJSON := testExecutor.ExportMetadata()
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}

	builtins := metadata["builtins"].(map[string]interface{})
	if _, ok := builtins["len"]; !ok {
		t.Error("len not found in builtins")
	}

	modules := metadata["modules"].(map[string]interface{})
	if _, ok := modules["fmt"]; !ok {
		t.Error("fmt module not found")
	}
	fmtMod := modules["fmt"].(map[string]interface{})
	fmtFuncs := fmtMod["functions"].(map[string]interface{})
	if _, ok := fmtFuncs["Printf"]; !ok {
		t.Error("fmt.Printf not found")
	}

	if _, ok := modules["main"]; !ok {
		t.Error("main module not found")
	}
	mainMod := modules["main"].(map[string]interface{})
	mainFuncs := mainMod["functions"].(map[string]interface{})
	if sig, ok := mainFuncs["Add"]; ok {
		t.Logf("Add signature: %v", sig)
		sigStr := sig.(string)
		if !strings.Contains(sigStr, "Int64") {
			t.Errorf("Signature should contain types, got %s", sigStr)
		}
	} else {
		t.Error("Add function not found in main module")
	}

	mainConstants := mainMod["constants"].(map[string]interface{})
	if val, ok := mainConstants["VERSION"]; ok {
		if val != "1.0.0" {
			t.Errorf("Expected VERSION 1.0.0, got %v", val)
		}
	} else {
		t.Error("VERSION constant not found")
	}

	mainValues := mainMod["values"].(map[string]interface{})
	if typ, ok := mainValues["Total"]; !ok || typ != "Int64" {
		t.Fatalf("expected Total value type Int64, got %v", typ)
	}
	mainTypes := mainMod["types"].(map[string]interface{})
	if typ, ok := mainTypes["Count"]; !ok || typ != "main.Count" {
		t.Fatalf("expected Count type main.Count, got %v", typ)
	}
}
