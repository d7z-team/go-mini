package runtime

import (
	"context"
	"errors"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type FiberFrameKind uint8

const (
	FrameRoot FiberFrameKind = iota
	FrameGo
	FrameModuleInit
)

type FiberFrame struct {
	Executor *Executor
	Session  *StackContext
	Kind     FiberFrameKind
	OnDone   func(*FiberFrame) error
	OnError  func(*FiberFrame, error) error
	Cleanup  bool
}

type VMFiber struct {
	ID     uint32
	Frames []*FiberFrame
}

func (f *VMFiber) CurrentFrame() *FiberFrame {
	if f == nil || len(f.Frames) == 0 {
		return nil
	}
	return f.Frames[len(f.Frames)-1]
}

func (f *VMFiber) PushFrame(frame *FiberFrame) error {
	if f == nil || frame == nil || frame.Executor == nil || frame.Session == nil {
		return errors.New("invalid fiber frame")
	}
	f.Frames = append(f.Frames, frame)
	return nil
}

func (f *VMFiber) PopFrame() *FiberFrame {
	if f == nil || len(f.Frames) == 0 {
		return nil
	}
	frame := f.Frames[len(f.Frames)-1]
	f.Frames[len(f.Frames)-1] = nil
	f.Frames = f.Frames[:len(f.Frames)-1]
	return frame
}

type ffiCompletion struct {
	token uint64
	ret   []byte
	err   error
}

type suspendedFiber struct {
	Fiber  *VMFiber
	Frame  *FiberFrame
	Resume Task
	Cancel func()
}

type FiberScheduler struct {
	runID     uint64
	nextID    uint32
	nextToken uint64
	current   *VMFiber
	runq      []*VMFiber
	pending   map[uint64]*suspendedFiber
	completed chan ffiCompletion
	stopped   bool
}

func NewFiberScheduler() *FiberScheduler {
	return &FiberScheduler{
		pending:   make(map[uint64]*suspendedFiber),
		completed: make(chan ffiCompletion, 1024),
	}
}

func (s *FiberScheduler) Reset(root *StackContext, exec *Executor) (*VMFiber, error) {
	if s == nil || root == nil || exec == nil {
		return nil, errors.New("invalid fiber root")
	}
	s.Stop()
	s.runID++
	s.nextID = 1
	s.nextToken = 0
	s.current = nil
	s.stopped = false
	rootFiber := &VMFiber{
		ID: s.nextID,
		Frames: []*FiberFrame{{
			Executor: exec,
			Session:  root,
			Kind:     FrameRoot,
		}},
	}
	s.runq = []*VMFiber{rootFiber}
	return rootFiber, nil
}

func (s *FiberScheduler) Current() *VMFiber {
	if s == nil {
		return nil
	}
	return s.current
}

func (s *FiberScheduler) Go(session *StackContext, exec *Executor) (*VMFiber, error) {
	if s == nil || session == nil || exec == nil {
		return nil, errors.New("invalid go fiber")
	}
	if s.stopped {
		return nil, errors.New("cannot start go fiber after scheduler stopped")
	}
	s.nextID++
	fiber := &VMFiber{
		ID: s.nextID,
		Frames: []*FiberFrame{{
			Executor: exec,
			Session:  session,
			Kind:     FrameGo,
			Cleanup:  true,
		}},
	}
	s.runq = append(s.runq, fiber)
	return fiber, nil
}

func (s *FiberScheduler) PushFrame(frame *FiberFrame) error {
	if s == nil || s.current == nil {
		return errors.New("missing current fiber")
	}
	return s.current.PushFrame(frame)
}

func (s *FiberScheduler) EnqueueFiber(fiber *VMFiber) {
	if s == nil || fiber == nil || s.stopped {
		return
	}
	s.runq = append(s.runq, fiber)
}

func (s *FiberScheduler) PrepareFFI(resume Task) (uint64, ffigo.WireCompletion, error) {
	if s == nil || s.current == nil {
		return 0, nil, errors.New("missing current fiber")
	}
	frame := s.current.CurrentFrame()
	if frame == nil {
		return 0, nil, errors.New("missing current fiber frame")
	}
	s.nextToken++
	token := (s.runID << 32) | s.nextToken
	s.pending[token] = &suspendedFiber{
		Fiber:  s.current,
		Frame:  frame,
		Resume: resume,
	}
	return token, ffiCompletionSink{scheduler: s, token: token}, nil
}

func (s *FiberScheduler) CommitFFI(token uint64, cancel func()) error {
	if s == nil || s.current == nil {
		return errors.New("missing current fiber")
	}
	pending := s.pending[token]
	if pending == nil {
		return errors.New("missing pending ffi fiber")
	}
	pending.Cancel = cancel
	s.current = nil
	return nil
}

func (s *FiberScheduler) AbortFFI(token uint64) {
	if s == nil {
		return
	}
	delete(s.pending, token)
}

func (s *FiberScheduler) ParkCurrent() (*VMFiber, *FiberFrame, error) {
	if s == nil || s.current == nil {
		return nil, nil, errors.New("missing current fiber")
	}
	frame := s.current.CurrentFrame()
	if frame == nil {
		return nil, nil, errors.New("missing current fiber frame")
	}
	fiber := s.current
	s.current = nil
	return fiber, frame, nil
}

func (s *FiberScheduler) YieldCurrent() error {
	if s == nil || s.current == nil {
		return nil
	}
	s.runq = append(s.runq, s.current)
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
	for _, pending := range s.pending {
		if pending != nil && pending.Cancel != nil {
			pending.Cancel()
		}
	}
	s.current = nil
	s.runq = nil
	s.pending = make(map[uint64]*suspendedFiber)
	s.stopped = true
}

func (s *FiberScheduler) completeWire(token uint64, ret []byte, err error) bool {
	if s == nil {
		return false
	}
	var owned []byte
	if ret != nil {
		owned = append([]byte(nil), ret...)
	}
	select {
	case s.completed <- ffiCompletion{token: token, ret: owned, err: err}:
		return true
	default:
		return false
	}
}

func (s *FiberScheduler) Next(ctx context.Context) (*VMFiber, error) {
	if s == nil || s.stopped {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		s.drainCompletions()
		if len(s.runq) > 0 {
			fiber := s.runq[0]
			copy(s.runq, s.runq[1:])
			s.runq[len(s.runq)-1] = nil
			s.runq = s.runq[:len(s.runq)-1]
			s.current = fiber
			return fiber, nil
		}
		if len(s.pending) == 0 {
			return nil, nil
		}
		select {
		case completion := <-s.completed:
			s.complete(completion)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *FiberScheduler) drainCompletions() {
	for {
		select {
		case completion := <-s.completed:
			s.complete(completion)
		default:
			return
		}
	}
}

func (s *FiberScheduler) complete(completion ffiCompletion) {
	pending := s.pending[completion.token]
	if pending == nil {
		return
	}
	delete(s.pending, completion.token)
	if data, ok := pending.Resume.Data.(*ResumeFFIData); ok && data != nil {
		data.Ret = completion.ret
		data.Err = completion.err
	}
	pending.Frame.Session.TaskStack = append(pending.Frame.Session.TaskStack, pending.Resume)
	s.runq = append(s.runq, pending.Fiber)
}

type ffiCompletionSink struct {
	scheduler *FiberScheduler
	token     uint64
}

func (s ffiCompletionSink) CompleteWire(ret []byte, err error) bool {
	if s.scheduler == nil {
		return false
	}
	return s.scheduler.completeWire(s.token, ret, err)
}
