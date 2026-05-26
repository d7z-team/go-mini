package tests

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestImportStrictnessRegression(t *testing.T) {
	e := engine.NewMiniExecutor()
	testsurface.UseRoute(t, e, "mock.Getenv", nil, 1, runtime.MustParseRuntimeFuncSig("function(String) String"), "")

	t.Run("Missing Import Should Fail", func(t *testing.T) {
		code := `
		package main
		func main() {
			mock.ReadFile("test.txt")
		}
		`
		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Fatal("expected error for missing import 'mock', but got none")
		}

		expected := "variable mock does not exist"
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error message mismatch.\ngot: %v\nwant: %s", err, expected)
		}
	})

	t.Run("Import Present But Member Missing Should Fail", func(t *testing.T) {
		code := `
		package main
		import "mock"
		func main() {
			mock.NonExistentFunc()
		}
		`
		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Fatal("expected error for missing member in package 'mock', but got none")
		}

		expected := "package mock has no member NonExistentFunc"
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error message mismatch.\ngot: %v\nwant: %s", err, expected)
		}
	})

	t.Run("Correct Import Should Pass", func(t *testing.T) {
		code := `
		package main
		import "mock"
		func main() {
			// Just check if it compiles
			_ = mock.Getenv("PATH")
		}
		`
		_, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("expected correct import to pass, but got error: %v", err)
		}
	})
}
