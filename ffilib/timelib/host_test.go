package timelib

import (
	"context"
	"testing"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestHostSleepCancelStopsDetachedTimer(t *testing.T) {
	async := (&TimeHost{}).Sleep(context.Background(), int64(200*time.Millisecond))
	doneCh := make(chan error, 1)
	wait, err := async.Start(context.Background(), hostTestCompletion{done: doneCh})
	if err != nil {
		t.Fatal(err)
	}
	if wait == nil {
		t.Fatal("expected wait handle")
	}
	start := time.Now()
	wait.Cancel()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("unexpected completion error: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Fatalf("expected canceled detached sleep to complete promptly, took %s", elapsed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for canceled detached sleep")
	}
}

type hostTestCompletion struct {
	done chan<- error
}

func (c hostTestCompletion) Complete(_ ffigo.Void, err error) bool {
	select {
	case c.done <- err:
	default:
	}
	return true
}
