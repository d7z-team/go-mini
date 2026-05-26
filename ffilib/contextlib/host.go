package contextlib

import (
	gocontext "context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ModuleHost struct{}

func (h *ModuleHost) Canceled() error {
	return gocontext.Canceled
}

func (h *ModuleHost) DeadlineExceeded() error {
	return gocontext.DeadlineExceeded
}

func (h *ModuleHost) NewTimer(ns int64) *Timer {
	return &Timer{impl: newTimerState(ns)}
}

func (h *ModuleHost) ValidValueKey(key any) bool {
	if key == nil {
		return false
	}
	return reflect.TypeOf(key).Comparable()
}

type timerWaiterID uint64

type timerState struct {
	mu      sync.Mutex
	timer   *time.Timer
	nextID  timerWaiterID
	fired   bool
	stopped bool
	waiters map[timerWaiterID]ffigo.Completion[bool]
}

func newTimerState(ns int64) *timerState {
	state := &timerState{}
	if ns <= 0 {
		state.fired = true
		return state
	}
	state.timer = time.AfterFunc(time.Duration(ns), state.fire)
	return state
}

func (s *timerState) fire() {
	if s == nil {
		return
	}
	waiters := s.finishLockedState(true)
	for _, done := range waiters {
		done.Complete(true, nil)
	}
}

func (s *timerState) wait() ffigo.Async[bool] {
	return ffigo.AsyncFunc[bool](func(_ gocontext.Context, done ffigo.Completion[bool]) (ffigo.WaitHandle, error) {
		if s == nil {
			done.Complete(false, nil)
			return nil, nil
		}

		s.mu.Lock()
		switch {
		case s.fired:
			s.mu.Unlock()
			done.Complete(true, nil)
			return nil, nil
		case s.stopped:
			s.mu.Unlock()
			done.Complete(false, nil)
			return nil, nil
		}

		s.nextID++
		id := s.nextID
		if s.waiters == nil {
			s.waiters = make(map[timerWaiterID]ffigo.Completion[bool])
		}
		s.waiters[id] = done
		s.mu.Unlock()

		cancel := func() {
			s.mu.Lock()
			delete(s.waiters, id)
			s.mu.Unlock()
		}
		return ffigo.NewWaitHandle(ffigo.WaitExternal, "context/internal.Timer.Wait", cancel), nil
	})
}

func (s *timerState) stop() bool {
	if s == nil {
		return false
	}
	stoppedTimer := false
	if s.timer != nil {
		stoppedTimer = s.timer.Stop()
	}
	waiters := s.finishLockedState(false)
	for _, done := range waiters {
		done.Complete(false, nil)
	}
	return stoppedTimer
}

func (s *timerState) finishLockedState(fired bool) []ffigo.Completion[bool] {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fired || s.stopped {
		return nil
	}
	if fired {
		s.fired = true
	} else {
		s.stopped = true
	}

	waiters := make([]ffigo.Completion[bool], 0, len(s.waiters))
	for id, done := range s.waiters {
		waiters = append(waiters, done)
		delete(s.waiters, id)
	}
	return waiters
}

func (s *timerState) String() string {
	if s == nil {
		return "context/internal.Timer nil"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("context/internal.Timer fired=%t stopped=%t waiters=%d", s.fired, s.stopped, len(s.waiters))
}
