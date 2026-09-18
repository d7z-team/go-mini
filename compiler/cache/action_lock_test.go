package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActionLockCancellationAndClear(t *testing.T) {
	action := testCacheAction("example/main", "main", nil, nil)
	for _, transient := range []bool{false, true} {
		name := "store"
		store := New(NewMemoryBackend())
		locks := store.(Store).locks
		clearCache := func() {}
		if transient {
			name = "transient"
			local := NewTransient(TransientConfig{})
			store, locks, clearCache = local, &local.locks, local.Clear
		}
		t.Run(name, func(t *testing.T) {
			release, err := store.LockCompile(t.Context(), action)
			if err != nil {
				t.Fatal(err)
			}
			clearCache()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			second, err := store.LockCompile(ctx, action)
			if second != nil {
				second()
				t.Error("waiter acquired a held action after Clear")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("wait error = %v", err)
			}
			if len(locks.flights) != 1 {
				t.Errorf("owner flight lost: %d", len(locks.flights))
			}
			release()
			if len(locks.flights) != 0 {
				t.Fatalf("released flights retained: %d", len(locks.flights))
			}
			third, err := store.LockCompile(t.Context(), action)
			if err != nil {
				t.Fatal(err)
			}
			third()
		})
	}
}

func TestActionLocksReleaseConcurrentWaiters(t *testing.T) {
	locks := &actionLocks{}
	release, err := locks.acquire(t.Context(), "action")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 16)
	for range 16 {
		go func() {
			unlock, err := locks.acquire(t.Context(), "action")
			if err == nil {
				unlock()
			}
			results <- err
		}()
	}
	release()
	for range 16 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(locks.flights) != 0 {
		t.Fatal("completed waiters retained")
	}
}

func TestStoreLockRequiresInitializedStore(t *testing.T) {
	var store Store
	if release, err := store.LockCompile(t.Context(), testCacheAction("example/main", "main", nil, nil)); err == nil || release != nil {
		t.Fatalf("zero Store lock = %v, %v", release != nil, err)
	}
}
