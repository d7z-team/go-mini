package debugger

import (
	"context"
	"sync/atomic"

	"gopkg.d7z.net/go-mini/core/ast"
)

type Command string

const (
	CmdContinue Command = "continue"
	CmdStepInto Command = "step"
)

type Event struct {
	Loc       *ast.Position
	Variables map[string]string
}

type Session struct {
	Breakpoints    map[int]bool
	EventChan      chan *Event
	CommandChan    chan Command
	isStepping     bool
	pauseRequested int32
}

func NewSession() *Session {
	return &Session{
		Breakpoints: make(map[int]bool),
		EventChan:   make(chan *Event),
		CommandChan: make(chan Command),
	}
}

func (s *Session) AddBreakpoint(line int) {
	s.Breakpoints[line] = true
}

func (s *Session) RemoveBreakpoint(line int) {
	delete(s.Breakpoints, line)
}

func (s *Session) HasBreakpoint(line int) bool {
	return s.Breakpoints[line]
}

func (s *Session) IsStepping() bool {
	return s.isStepping
}

func (s *Session) SetStepping(v bool) {
	s.isStepping = v
}

// RequestPause 宿主调用：请求 VM 在下一条语句立即停止执行
func (s *Session) RequestPause() {
	atomic.StoreInt32(&s.pauseRequested, 1)
}

// ShouldTrigger 内部方法：检查是否应该触发暂停（断点、单步或人工请求暂停）
func (s *Session) ShouldTrigger(line int) bool {
	// 1. 检查断点
	if s.HasBreakpoint(line) {
		return true
	}
	// 2. 检查单步模式
	if s.IsStepping() {
		return true
	}
	// 3. 检查人工异步暂停请求
	if atomic.CompareAndSwapInt32(&s.pauseRequested, 1, 0) {
		return true
	}
	return false
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
