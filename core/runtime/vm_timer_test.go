package runtime

import (
	"testing"
	"time"
)

func TestVMTimerRequiresService(t *testing.T) {
	timer := NewVMTimer(nil, time.Hour)
	if timer == nil {
		t.Fatal("expected timer instance")
	}
	if timer.service != nil {
		t.Fatal("timer without service should not retain a timer service")
	}
	if timer.Stop() {
		t.Fatal("timer without service should already be inactive")
	}
}

func TestVMTimerServiceStopSucceedsWithoutWaiters(t *testing.T) {
	service := NewVMTimerService(NewVMClock())
	timer := NewVMTimer(service, time.Hour)
	if timer == nil {
		t.Fatal("expected timer")
	}
	if !timer.Stop() {
		t.Fatal("expected first stop to succeed")
	}
	if timer.Stop() {
		t.Fatal("expected second stop to report inactive timer")
	}
	service.Stop()
}
