package e2e_test

import (
	"context"
	"testing"
)

func TestStandardLibraryTypeMismatch(t *testing.T) {
	executor := newStdExecutor()

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

	results, err := prog.Eval(context.Background(), "GetRes()", nil)
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Eval returned %d values, want 1", len(results))
	}
	res := results[0]

	if res.Str != "ok" {
		t.Errorf("Expected 'ok', got '%s'", res.Str)
	}
}
