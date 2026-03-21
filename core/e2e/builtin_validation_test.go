package e2e

import (
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestInvalidMakeCompileError(t *testing.T) {
	executor := engine.NewMiniExecutor()

	t.Run("String Literal as Type", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic for string literal in make")
			} else if !strings.Contains(fmt.Sprint(r), "第一个参数必须是类型") {
				t.Errorf("unexpected panic message: %v", r)
			}
		}()
		code := `
		package main
		func main() {
			m := make("InvalidType")
		}
		`
		_, _ = executor.NewRuntimeByGoCode(code)
	})
}

func TestNewBuiltinCompileError(t *testing.T) {
	executor := engine.NewMiniExecutor()

	t.Run("String Literal as Type", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic for string literal in new")
			} else if !strings.Contains(fmt.Sprint(r), "第一个参数必须是类型") {
				t.Errorf("unexpected panic message: %v", r)
			}
		}()
		code := `
		package main
		func main() {
			p := new("InvalidType")
		}
		`
		_, _ = executor.NewRuntimeByGoCode(code)
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
}
