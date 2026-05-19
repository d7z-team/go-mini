package runtime

import (
	"context"
	"errors"
	"time"
)

type VMFiber struct {
	ID      uint32
	Session *StackContext
	wakeAt  time.Time
}

type FiberScheduler struct {
	nextID   uint32
	current  *VMFiber
	runq     []*VMFiber
	sleepers []*VMFiber
	stopped  bool
}

func NewFiberScheduler() *FiberScheduler {
	return &FiberScheduler{}
}

func (s *FiberScheduler) Reset(root *StackContext) (*VMFiber, error) {
	if s == nil || root == nil {
		return nil, errors.New("invalid fiber root")
	}
	s.nextID = 1
	s.current = nil
	s.sleepers = nil
	s.stopped = false
	rootFiber := &VMFiber{ID: s.nextID, Session: root}
	s.runq = []*VMFiber{rootFiber}
	return rootFiber, nil
}

func (s *FiberScheduler) Current() *VMFiber {
	if s == nil {
		return nil
	}
	return s.current
}

func (s *FiberScheduler) Go(session *StackContext) (*VMFiber, error) {
	if s == nil || session == nil {
		return nil, errors.New("invalid go fiber")
	}
	if s.stopped {
		return nil, errors.New("cannot start go fiber after scheduler stopped")
	}
	s.nextID++
	fiber := &VMFiber{ID: s.nextID, Session: session}
	s.runq = append(s.runq, fiber)
	return fiber, nil
}

func (s *FiberScheduler) YieldCurrent() error {
	if s == nil || s.current == nil {
		return nil
	}
	s.runq = append(s.runq, s.current)
	s.current = nil
	return nil
}

func (s *FiberScheduler) SleepCurrent(d time.Duration) error {
	if s == nil || s.current == nil {
		if d > 0 {
			time.Sleep(d)
		}
		return nil
	}
	if d <= 0 {
		return s.YieldCurrent()
	}
	s.current.wakeAt = time.Now().Add(d)
	s.sleepers = append(s.sleepers, s.current)
	s.current = nil
	return nil
}

func (s *FiberScheduler) FinishCurrent() {
	if s == nil {
		return
	}
	s.current = nil
}

func (s *FiberScheduler) Stop() {
	if s == nil {
		return
	}
	s.current = nil
	s.runq = nil
	s.sleepers = nil
	s.stopped = true
}

func (s *FiberScheduler) Next(ctx context.Context) (*VMFiber, error) {
	if s == nil || s.stopped {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		now := time.Now()
		s.wakeReady(now)
		if len(s.runq) > 0 {
			fiber := s.runq[0]
			copy(s.runq, s.runq[1:])
			s.runq[len(s.runq)-1] = nil
			s.runq = s.runq[:len(s.runq)-1]
			s.current = fiber
			return fiber, nil
		}
		if len(s.sleepers) == 0 {
			return nil, nil
		}
		delay := s.nextWakeDelay(now)
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

func (s *FiberScheduler) wakeReady(now time.Time) {
	if len(s.sleepers) == 0 {
		return
	}
	pending := s.sleepers[:0]
	for _, fiber := range s.sleepers {
		if fiber == nil {
			continue
		}
		if fiber.wakeAt.IsZero() || !fiber.wakeAt.After(now) {
			fiber.wakeAt = time.Time{}
			s.runq = append(s.runq, fiber)
			continue
		}
		pending = append(pending, fiber)
	}
	s.sleepers = pending
}

func (s *FiberScheduler) nextWakeDelay(now time.Time) time.Duration {
	var wake time.Time
	for _, fiber := range s.sleepers {
		if fiber == nil || fiber.wakeAt.IsZero() {
			return 0
		}
		if wake.IsZero() || fiber.wakeAt.Before(wake) {
			wake = fiber.wakeAt
		}
	}
	if wake.IsZero() {
		return 0
	}
	return time.Until(wake)
}
