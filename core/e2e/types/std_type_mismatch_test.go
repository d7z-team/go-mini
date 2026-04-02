package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestStandardLibraryTypeMismatch(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main
import "time"

func printTime(t *time.Time) string {
	if t == nil { return "nil" }
	return "ok"
}

func GetRes() string {
	now := time.Now()
	return printTime(now)
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	res, err := prog.Eval(context.Background(), "GetRes()", nil)
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}

	if res.Str != "ok" {
		t.Errorf("Expected 'ok', got '%s'", res.Str)
	}
}
