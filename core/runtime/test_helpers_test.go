package runtime

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func newExecutor(t *testing.T, program *ast.ProgramStmt) *Executor {
	t.Helper()

	prepared, err := PrepareProgram(program)
	if err != nil {
		t.Fatalf("prepare program failed: %v", err)
	}
	exec, err := NewExecutorFromPrepared(program, prepared)
	if err != nil {
		t.Fatalf("new executor from prepared failed: %v", err)
	}
	return exec
}
