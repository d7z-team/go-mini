package debugger

import (
	"context"
	"sync"
	"sync/atomic"
)

type Command string

const (
	CmdContinue Command = "continue"
	CmdStepInto Command = "step"
)

type Event struct {
	// ExecutionContextID identifies which VM execution context triggered the current all-stop pause.
	ExecutionContextID uint32
	Loc                *Position
	Variables          map[string]string
}

type Position struct {
	F  string
	L  int
	C  int
	EL int
	EC int
}

type Session struct {
	mu          sync.RWMutex
	breakpoints map[int]struct{}
	events      chan *Event
	commands    chan Command
	isStepping  bool

	pauseRequested atomic.Bool
}

func NewSession() *Session {
	return &Session{
		breakpoints: make(map[int]struct{}),
		events:      make(chan *Event),
		commands:    make(chan Command),
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

func (s *Session) IsStepping() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isStepping
}

func (s *Session) SetStepping(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isStepping = v
}

// RequestPause 宿主调用：请求 VM 在下一条语句立即停止执行
func (s *Session) RequestPause() {
	s.pauseRequested.Store(true)
}

func (s *Session) Events() <-chan *Event {
	return s.events
}

func (s *Session) Continue() {
	s.commands <- CmdContinue
}

func (s *Session) StepInto() {
	s.commands <- CmdStepInto
}

// Pause publishes an all-stop event and blocks until the debugger host resumes the VM.
func (s *Session) Pause(event *Event) Command {
	s.events <- event
	return <-s.commands
}

// ShouldTrigger 内部方法：检查是否应该触发暂停（断点、单步或人工请求暂停）
func (s *Session) ShouldTrigger(line int) bool {
	s.mu.RLock()
	_, hitBreakpoint := s.breakpoints[line]
	stepping := s.isStepping
	s.mu.RUnlock()

	return hitBreakpoint || stepping || s.pauseRequested.Swap(false)
}

type contextKey string

const Key contextKey = "go-mini-debugger"

// WithDebugger 附加调试器会话到上下文
func WithDebugger(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, Key, s)
}

// GetDebugger 从上下文中获取调试器会话
func GetDebugger(ctx context.Context) *Session {
	if s, ok := ctx.Value(Key).(*Session); ok {
		return s
	}
	return nil
}
