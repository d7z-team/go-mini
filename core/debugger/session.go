package debugger

import (
	"context"
	"errors"
	"sync"

	miniruntime "gopkg.d7z.net/go-mini/core/runtime"
)

type Event = miniruntime.DebugEvent

type Position = miniruntime.DebugPosition

var ErrSessionClosed = errors.New("debugger session closed")

type Session struct {
	mu          sync.RWMutex
	breakpoints map[int]struct{}
	stepRunID   uint64
	stepActive  bool

	eventMu     sync.Mutex
	eventNotify chan struct{}
	eventQueue  []*Event
	closed      bool
}

func NewSession() *Session {
	return &Session{
		breakpoints: make(map[int]struct{}),
		eventNotify: make(chan struct{}),
	}
}

func (s *Session) AddBreakpoint(line int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breakpoints[line] = struct{}{}
}

func (s *Session) RemoveBreakpoint(line int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.breakpoints, line)
}

func (s *Session) HasBreakpoint(line int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.breakpoints[line]
	return ok
}

func (s *Session) HasStep(runID uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stepActive && s.stepRunID == runID
}

func (s *Session) RequestStep(runID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stepRunID = runID
	s.stepActive = true
}

func (s *Session) ClearStep(runID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stepActive && s.stepRunID == runID {
		s.stepActive = false
		s.stepRunID = 0
	}
}

func (s *Session) Publish(event *Event) {
	if event == nil {
		return
	}
	s.eventMu.Lock()
	if s.closed {
		s.eventMu.Unlock()
		return
	}
	s.eventQueue = append(s.eventQueue, event)
	s.signalEventsLocked()
	s.eventMu.Unlock()
}

func (s *Session) NextEvent(ctx context.Context) (*Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		s.eventMu.Lock()
		if len(s.eventQueue) > 0 {
			event := s.eventQueue[0]
			copy(s.eventQueue, s.eventQueue[1:])
			s.eventQueue[len(s.eventQueue)-1] = nil
			s.eventQueue = s.eventQueue[:len(s.eventQueue)-1]
			s.eventMu.Unlock()
			return event, nil
		}
		if s.closed {
			s.eventMu.Unlock()
			return nil, ErrSessionClosed
		}
		notify := s.eventNotify
		s.eventMu.Unlock()

		select {
		case <-notify:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *Session) Close() {
	s.eventMu.Lock()
	if !s.closed {
		s.closed = true
		clear(s.eventQueue)
		s.eventQueue = nil
		close(s.eventNotify)
	}
	s.eventMu.Unlock()
}

func (s *Session) signalEventsLocked() {
	if s.closed {
		return
	}
	close(s.eventNotify)
	s.eventNotify = make(chan struct{})
}

func (s *Session) ShouldTrigger(runID uint64, line int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, hitBreakpoint := s.breakpoints[line]
	stepping := s.stepActive && s.stepRunID == runID
	return hitBreakpoint || stepping
}

func WithDebugger(ctx context.Context, s *Session) context.Context {
	return miniruntime.ContextWithDebugger(ctx, s)
}

func GetDebugger(ctx context.Context) *Session {
	s, _ := miniruntime.DebuggerFromContext(ctx).(*Session)
	return s
}
