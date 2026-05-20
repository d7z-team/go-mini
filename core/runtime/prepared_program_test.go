package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestNewExecutorFromPreparedRequiresPreparedProgram(t *testing.T) {
	_, err := NewExecutorFromPrepared(nil)
	if err == nil {
		t.Fatal("expected missing prepared program error")
	}
	if !strings.Contains(err.Error(), "missing prepared program") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewExecutorFromPreparedRejectsOrphanScopeExit(t *testing.T) {
	prepared := &PreparedProgram{
		Globals:   map[string]*PreparedGlobal{},
		Functions: map[string]*PreparedFunction{},
		MainTasks: []Task{{Op: OpScopeExit}},
	}
	_, err := NewExecutorFromPrepared(prepared)
	if err == nil {
		t.Fatal("expected invalid prepared program error")
	}
	if !strings.Contains(err.Error(), "exits without matching scope enter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntimeScopeExitReturnsErrorInsteadOfPanic(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	session.TaskStack = []Task{{Op: OpScopeExit}}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("scope exit should return an error, got panic: %v", r)
		}
	}()

	err := exec.Run(session)
	if err == nil {
		t.Fatal("expected scope exit error")
	}
	if !strings.Contains(err.Error(), "scope exit would leave root scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}
