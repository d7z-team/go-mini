package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestStringsLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	import "strings"

	func main() {
		s := "  Hello, Go-Mini!  "
		
		// Test TrimSpace
		trimmed := strings.TrimSpace(s)
		if trimmed != "Hello, Go-Mini!" {
			panic("TrimSpace failed: " + trimmed)
		}
		
		// Test Contains
		if !strings.Contains(trimmed, "Go-Mini") {
			panic("Contains failed")
		}
		
		// Test HasPrefix/HasSuffix
		if !strings.HasPrefix(trimmed, "Hello") {
			panic("HasPrefix failed")
		}
		if !strings.HasSuffix(trimmed, "!") {
			panic("HasSuffix failed")
		}
		
		// Test ToLower/ToUpper
		if strings.ToLower("ABC") != "abc" {
			panic("ToLower failed")
		}
		if strings.ToUpper("abc") != "ABC" {
			panic("ToUpper failed")
		}
		
		// Test ReplaceAll
		replaced := strings.ReplaceAll(trimmed, "Go-Mini", "World")
		if replaced != "Hello, World!" {
			panic("ReplaceAll failed: " + replaced)
		}
		
		// Test Split/Join
		parts := strings.Split("a,b,c", ",")
		if len(parts) != 3 {
			panic("Split failed")
		}
		joined := strings.Join(parts, "-")
		if joined != "a-b-c" {
			panic("Join failed: " + joined)
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
