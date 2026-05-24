package ffigo

import "testing"

func TestHandleRegistryTransactionRollback(t *testing.T) {
	registry := NewHandleRegistry()
	tx := registry.BeginTransaction()
	id := tx.RegisterPinnedTyped(&struct{}{}, "demo.Handle")
	if id == 0 {
		t.Fatal("expected transaction to allocate a handle")
	}
	tx.Rollback()

	if _, ok := registry.Get(id); ok {
		t.Fatalf("rolled back handle %d should not be visible", id)
	}
}

func TestHandleRegistryTransactionCommit(t *testing.T) {
	registry := NewHandleRegistry()
	tx := registry.BeginTransaction()
	value := &struct{}{}
	id := tx.RegisterPinnedTyped(value, "demo.Handle")
	if id == 0 {
		t.Fatal("expected transaction to allocate a handle")
	}
	tx.Commit()

	got, ok := registry.Get(id)
	if !ok || got != value {
		t.Fatalf("committed handle mismatch: got %#v ok=%v", got, ok)
	}
}

func TestHandleRegistryTransactionProxyRollback(t *testing.T) {
	registry := NewHandleRegistry()
	tx := registry.BeginTransaction()
	id := tx.Registry.RegisterTyped(&struct{}{}, "demo.Handle")
	if id == 0 {
		t.Fatal("expected transaction proxy to allocate a handle")
	}
	tx.Rollback()

	if _, ok := registry.Get(id); ok {
		t.Fatalf("rolled back proxy handle %d should not be visible", id)
	}
}

func TestHandleRegistryTransactionProxyDelegatesAfterCommit(t *testing.T) {
	registry := NewHandleRegistry()
	tx := registry.BeginTransaction()
	tx.Commit()

	value := &struct{}{}
	id := tx.Registry.RegisterTyped(value, "demo.Handle")
	got, ok := registry.Get(id)
	if !ok || got != value {
		t.Fatalf("post-commit proxy handle mismatch: got %#v ok=%v", got, ok)
	}
}
