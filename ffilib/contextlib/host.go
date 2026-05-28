package contextlib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ModuleHost struct{}

func (h *ModuleHost) Canceled() error {
	return context.Canceled
}

func (h *ModuleHost) DeadlineExceeded() error {
	return context.DeadlineExceeded
}

func (h *ModuleHost) NewTimer(ctx context.Context, ns int64) *Timer {
	_ = ctx
	return &Timer{impl: newTimerState(ns)}
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

func (s *timerState) wait() ffigo.Async[bool] {
	return ffigo.AsyncFunc[bool](func(ctx context.Context, done ffigo.Completion[bool]) (ffigo.WaitHandle, error) {
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
			s.cancelWaiter(id)
		}
		return ffigo.NewWaitHandle(ffigo.WaitExternal, "context/internal.Timer.Wait", cancel), nil
	})
}

func (s *timerState) cancelWaiter(id timerWaiterID) {
	if s == nil {
		return
	}
	var timer *time.Timer
	s.mu.Lock()
	if s.waiters != nil {
		delete(s.waiters, id)
	}
	if len(s.waiters) == 0 && !s.fired && !s.stopped {
		s.stopped = true
		timer = s.timer
		s.timer = nil
	}
	s.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

func (s *timerState) stop() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.fired || s.stopped {
		s.mu.Unlock()
		return false
	}
	s.stopped = true
	timer := s.timer
	s.timer = nil
	waiters := make([]ffigo.Completion[bool], 0, len(s.waiters))
	for id, done := range s.waiters {
		waiters = append(waiters, done)
		delete(s.waiters, id)
	}
	s.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	for _, done := range waiters {
		done.Complete(false, nil)
	}
	return true
}

func (s *timerState) fire() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.fired || s.stopped {
		s.mu.Unlock()
		return
	}
	s.fired = true
	s.timer = nil
	waiters := make([]ffigo.Completion[bool], 0, len(s.waiters))
	for id, done := range s.waiters {
		waiters = append(waiters, done)
		delete(s.waiters, id)
	}
	s.mu.Unlock()
	for _, done := range waiters {
		done.Complete(true, nil)
	}
}

func (s *timerState) String() string {
	if s == nil {
		return "context/internal.Timer nil"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("context/internal.Timer fired=%t stopped=%t waiters=%d", s.fired, s.stopped, len(s.waiters))
}
