package engine_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func executeAndSnapshot(t *testing.T, prog *engine.ExecutableProgram) *runtime.SharedStateSnapshot {
	t.Helper()

	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	snapshot := prog.SharedState()
	if snapshot == nil {
		t.Fatal("expected shared state snapshot")
	}
	return snapshot
}
