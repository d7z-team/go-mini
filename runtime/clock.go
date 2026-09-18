package runtime

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Clock supplies wall time and alarms to one Instance. Implementations may use
// real or virtual time, but callbacks must be safe to invoke from any goroutine.
type Clock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) ClockTimer
}

// ClockTimer stops an alarm registered with a Clock.
type ClockTimer interface {
	Stop() bool
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(duration time.Duration, callback func()) ClockTimer {
	return time.AfterFunc(duration, callback)
}

type manualAlarm struct {
	clock    *ManualClock
	deadline time.Time
	order    uint64
	callback func()
	active   bool
}

func (alarm *manualAlarm) Stop() bool {
	if alarm == nil || alarm.clock == nil {
		return false
	}
	alarm.clock.mu.Lock()
	defer alarm.clock.mu.Unlock()
	if !alarm.active {
		return false
	}
	alarm.active = false
	delete(alarm.clock.alarms, alarm)
	return true
}

// ManualClock is a deterministic Clock advanced explicitly by its owner.
type ManualClock struct {
	mu        sync.Mutex
	now       time.Time
	nextOrder uint64
	alarms    map[*manualAlarm]struct{}
}

// NewManualClock returns a virtual clock starting at now.
func NewManualClock(now time.Time) *ManualClock {
	return &ManualClock{now: now, alarms: make(map[*manualAlarm]struct{})}
}

func (clock *ManualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *ManualClock) AfterFunc(duration time.Duration, callback func()) ClockTimer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.alarms == nil {
		clock.alarms = make(map[*manualAlarm]struct{})
	}
	if duration < 0 {
		duration = 0
	}
	clock.nextOrder++
	alarm := &manualAlarm{
		clock: clock, deadline: clock.now.Add(duration), order: clock.nextOrder,
		callback: callback, active: true,
	}
	clock.alarms[alarm] = struct{}{}
	return alarm
}

// Advance moves the clock forward and invokes every alarm due at the new time.
func (clock *ManualClock) Advance(duration time.Duration) error {
	if duration < 0 {
		return errors.New("clock advance must not be negative")
	}
	clock.mu.Lock()
	target := clock.now.Add(duration)
	callbacks := clock.advanceLocked(target)
	clock.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
	return nil
}

// AdvanceToNext moves the clock to its next alarm. It reports whether an alarm
// was pending.
func (clock *ManualClock) AdvanceToNext() bool {
	clock.mu.Lock()
	var target time.Time
	for alarm := range clock.alarms {
		if alarm.active && (target.IsZero() || alarm.deadline.Before(target)) {
			target = alarm.deadline
		}
	}
	if target.IsZero() {
		clock.mu.Unlock()
		return false
	}
	callbacks := clock.advanceLocked(target)
	clock.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
	return true
}

func (clock *ManualClock) advanceLocked(target time.Time) []func() {
	clock.now = target
	due := make([]*manualAlarm, 0)
	for alarm := range clock.alarms {
		if alarm.active && !alarm.deadline.After(target) {
			alarm.active = false
			delete(clock.alarms, alarm)
			due = append(due, alarm)
		}
	}
	sort.Slice(due, func(left, right int) bool {
		if due[left].deadline.Equal(due[right].deadline) {
			return due[left].order < due[right].order
		}
		return due[left].deadline.Before(due[right].deadline)
	})
	callbacks := make([]func(), 0, len(due))
	for _, alarm := range due {
		if alarm.callback != nil {
			callbacks = append(callbacks, alarm.callback)
		}
	}
	return callbacks
}
