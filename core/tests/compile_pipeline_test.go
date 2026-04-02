package engine_test

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestCompileArtifactDoesNotExecuteGlobalInit(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	compiledArtifact, err := testExecutor.CompileGoCode(`
package main

func explode() int {
	panic("boom")
	return 0
}

var bad = explode()
`)
	if err != nil {
		t.Fatalf("compile failed unexpectedly: %v", err)
	}

	testProgram, err := testExecutor.NewRuntimeByCompiled(compiledArtifact)
	if err != nil {
		t.Fatalf("runtime creation failed unexpectedly: %v", err)
	}

	if err := testProgram.Execute(context.Background()); err == nil {
		t.Fatal("expected runtime init failure, got nil")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected runtime error: %v", err)
	}
}
