package debugger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	miniruntime "gopkg.d7z.net/go-mini/core/runtime"
)

type Event = miniruntime.DebugEvent

type Position = miniruntime.DebugPosition

type Breakpoint = miniruntime.DebugBreakpoint

var ErrSessionClosed = errors.New("debugger session closed")

type breakpointKey struct {
	modulePath string
	file       string
	line       int
}

type stepState struct {
	runID              uint64
	executionContextID uint32
	mode               miniruntime.DebugStepMode
	modulePath         string
	file               string
	frameDepth         int
	active             bool
}

type Session struct {
	mu          sync.RWMutex
	breakpoints map[breakpointKey]struct{}
	step        stepState
	pausePoints map[uint64]miniruntime.DebugPoint

	eventMu     sync.Mutex
	eventNotify chan struct{}
	eventQueue  []*Event
	closed      bool
}

func NewSession() *Session {
	return &Session{
		breakpoints: make(map[breakpointKey]struct{}),
		pausePoints: make(map[uint64]miniruntime.DebugPoint),
		eventNotify: make(chan struct{}),
	}
}

func (s *Session) AddBreakpoint(bp Breakpoint) error {
	key, err := keyFromBreakpoint(bp)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breakpoints[key] = struct{}{}
	return nil
}

func (s *Session) RemoveBreakpoint(bp Breakpoint) error {
	key, err := keyFromBreakpoint(bp)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.breakpoints, key)
	return nil
}

func (s *Session) HasBreakpoint(bp Breakpoint) (bool, error) {
	key, err := keyFromBreakpoint(bp)
	if err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.breakpoints[key]
	return ok, nil
}

func (s *Session) HasStep(runID uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.step.active && s.step.runID == runID
}

func (s *Session) RequestStep(req miniruntime.DebugStepRequest) error {
	if req.RunID == 0 {
		return errors.New("debug step requires run id")
	}
	if req.Mode == "" {
		req.Mode = miniruntime.DebugStepInto
	}
	if req.Mode != miniruntime.DebugStepInto && req.Mode != miniruntime.DebugStepOver {
		return errors.New("unsupported debug step mode")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	point, ok := s.pausePoints[req.RunID]
	if !ok {
		return errors.New("debug step requires a paused source location")
	}
	s.step = stepState{
		runID:              req.RunID,
		executionContextID: point.ExecutionContextID,
		mode:               req.Mode,
		modulePath:         point.Loc.ModulePath,
		file:               point.Loc.F,
		frameDepth:         point.FrameDepth,
		active:             true,
	}
	return nil
}

func (s *Session) ClearStep(runID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.step.active && s.step.runID == runID {
		s.step = stepState{}
	}
}

func (s *Session) ClearPause(runID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pausePoints, runID)
}

func (s *Session) ClearRun(runID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.step.active && s.step.runID == runID {
		s.step = stepState{}
	}
	delete(s.pausePoints, runID)
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
	s.mu.Lock()
	if event.Loc != nil {
		s.pausePoints[event.RunID] = miniruntime.DebugPoint{
			RunID:              event.RunID,
			ExecutionContextID: event.ExecutionContextID,
			Loc:                *event.Loc,
			FrameDepth:         event.FrameDepth,
		}
	}
	s.mu.Unlock()

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

func (s *Session) Checkpoint(point miniruntime.DebugPoint) miniruntime.DebugDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hitBreakpoint := s.hasBreakpointLocked(point)
	stepMatched := s.step.active && s.step.runID == point.RunID && stepMatchesPoint(s.step, point)
	stepInterrupted := hitBreakpoint && s.step.active && s.step.runID == point.RunID
	if !hitBreakpoint && !stepMatched {
		return miniruntime.DebugDecision{}
	}
	decision := miniruntime.DebugDecision{
		Stop:      true,
		ClearStep: stepMatched || stepInterrupted,
	}
	if hitBreakpoint {
		decision.Reason = miniruntime.DebugStopBreakpoint
	} else {
		decision.Reason = miniruntime.DebugStopStep
	}
	return decision
}

func (s *Session) hasBreakpointLocked(point miniruntime.DebugPoint) bool {
	key := breakpointKey{
		modulePath: point.Loc.ModulePath,
		file:       point.Loc.F,
		line:       point.Loc.L,
	}
	_, ok := s.breakpoints[key]
	return ok
}

func keyFromBreakpoint(bp Breakpoint) (breakpointKey, error) {
	modulePath := strings.TrimSpace(bp.ModulePath)
	file := strings.TrimSpace(bp.File)
	if modulePath == "" {
		return breakpointKey{}, errors.New("debug breakpoint requires module path")
	}
	if file == "" {
		return breakpointKey{}, errors.New("debug breakpoint requires file")
	}
	if bp.Line <= 0 {
		return breakpointKey{}, fmt.Errorf("debug breakpoint requires positive line, got %d", bp.Line)
	}
	return breakpointKey{modulePath: modulePath, file: file, line: bp.Line}, nil
}

func stepMatchesPoint(step stepState, point miniruntime.DebugPoint) bool {
	if point.ExecutionContextID != step.executionContextID {
		return false
	}
	switch step.mode {
	case miniruntime.DebugStepOver:
		if point.FrameDepth < step.frameDepth {
			return true
		}
		return point.FrameDepth == step.frameDepth &&
			point.Loc.ModulePath == step.modulePath &&
			point.Loc.F == step.file
	default:
		return true
	}
}

func WithDebugger(ctx context.Context, s *Session) context.Context {
	return miniruntime.ContextWithDebugger(ctx, s)
}

func GetDebugger(ctx context.Context) *Session {
	s, _ := miniruntime.DebuggerFromContext(ctx).(*Session)
	return s
}
