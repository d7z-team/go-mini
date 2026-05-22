package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFFIModule(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
	package main

	func main() {
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
