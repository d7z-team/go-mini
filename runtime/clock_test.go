package runtime

import (
	"testing"
	"time"
)

func TestManualClockOrdersAndStopsAlarms(t *testing.T) {
	clock := NewManualClock(time.Unix(10, 0))
	var fired []int
	clock.AfterFunc(2*time.Second, func() { fired = append(fired, 2) })
	clock.AfterFunc(time.Second, func() { fired = append(fired, 1) })
	stopped := clock.AfterFunc(500*time.Millisecond, func() { fired = append(fired, 0) })
	if !stopped.Stop() || stopped.Stop() {
		t.Fatal("manual alarm Stop returned the wrong state")
	}
	if !clock.AdvanceToNext() || !clock.Now().Equal(time.Unix(11, 0)) {
		t.Fatalf("first deadline = %s", clock.Now())
	}
	if !clock.AdvanceToNext() || !clock.Now().Equal(time.Unix(12, 0)) {
		t.Fatalf("second deadline = %s", clock.Now())
	}
	if clock.AdvanceToNext() {
		t.Fatal("manual clock reported an alarm after all alarms fired")
	}
	if len(fired) != 2 || fired[0] != 1 || fired[1] != 2 {
		t.Fatalf("alarm order = %v", fired)
	}
}

func TestManualClockFiresSameDeadlineInRegistrationOrder(t *testing.T) {
	clock := NewManualClock(time.Unix(20, 0))
	var fired []int
	for index := 0; index < 3; index++ {
		value := index
		clock.AfterFunc(time.Second, func() { fired = append(fired, value) })
	}
	if err := clock.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 3 || fired[0] != 0 || fired[1] != 1 || fired[2] != 2 {
		t.Fatalf("same-deadline order = %v", fired)
	}
}
