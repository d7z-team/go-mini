package e2e

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestImportStrictnessRegression(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.InjectStandardLibraries()

	t.Run("Missing Import Should Fail", func(t *testing.T) {
		code := `
		package main
		func main() {
			os.ReadFile("test.txt")
		}
		`
		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Fatal("expected error for missing import 'os', but got none")
		}

		expected := "变量 os 不存在"
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error message mismatch.\ngot: %v\nwant: %s", err, expected)
		}
	})

	t.Run("Import Present But Member Missing Should Fail", func(t *testing.T) {
		code := `
		package main
		import "os"
		func main() {
			os.NonExistentFunc()
		}
		`
		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Fatal("expected error for missing member in package 'os', but got none")
		}

		expected := "包 os 不存在成员 NonExistentFunc"
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error message mismatch.\ngot: %v\nwant: %s", err, expected)
		}
	})

	t.Run("Correct Import Should Pass", func(t *testing.T) {
		code := `
		package main
		import "os"
		func main() {
			// Just check if it compiles
			_ = os.Getenv("PATH")
		}
		`
		_, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("expected correct import to pass, but got error: %v", err)
		}
	})
}
