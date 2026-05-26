package contextlib

import (
	"context"
	"testing"
)

type boolCompletion struct {
	value bool
	calls int
}

func (c *boolCompletion) Complete(value bool, err error) bool {
	c.value = value
	c.calls++
	return true
}

func TestZeroValueTimerIsSafe(t *testing.T) {
	var timer Timer
	if timer.Stop() {
		t.Fatal("zero value timer should not report a stopped host timer")
	}
	done := &boolCompletion{}
	handle, err := timer.Wait().Start(context.Background(), done)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if handle != nil {
		t.Fatalf("zero value timer wait should complete synchronously: %#v", handle)
	}
	if done.calls != 1 || done.value {
		t.Fatalf("unexpected zero value timer completion: calls=%d value=%v", done.calls, done.value)
	}
}
