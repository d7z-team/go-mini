package tests

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestInvalidMakeCompileError(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	t.Run("String Literal as Type", func(t *testing.T) {
		code := `
		package main
		func main() {
			m := make("InvalidType")
		}
		`
		_, err := executor.NewRuntimeByGoCode(code)
		if err == nil || !strings.Contains(err.Error(), "first argument must be a type") {
			t.Fatalf("expected compile error for string literal in make, got %v", err)
		}
	})
}

func TestNewBuiltinCompileError(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	t.Run("String Literal as Type", func(t *testing.T) {
		code := `
		package main
		func main() {
			p := new("InvalidType")
		}
		`
		_, err := executor.NewRuntimeByGoCode(code)
		if err == nil || !strings.Contains(err.Error(), "first argument must be a type") {
			t.Fatalf("expected compile error for string literal in new, got %v", err)
		}
	})

	t.Run("Dynamic Variable as Type", func(t *testing.T) {
		code := `
		package main
		func main() {
			tName := "Int64"
			p := new(tName)
		}
		`
		// This should fail because tName is not a type in the identifier scope of the converter
		_, err := executor.NewRuntimeByGoCode(code)
		if err == nil {
			t.Fatalf("expected error for dynamic variable as type")
		}
	})

	t.Run("Extra Argument", func(t *testing.T) {
		code := `
		package main
		func main() {
			_ = new(int64, 1)
		}
		`
		_, err := executor.NewRuntimeByGoCode(code)
		if err == nil || !strings.Contains(err.Error(), "new: requires exactly 1 argument") {
			t.Fatalf("expected arity error for new, got %v", err)
		}
	})
}

func TestCapRejectsStringCompileError(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main
	func main() {
		_ = cap("abc")
	}
	`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected cap string compile error")
	}
}
