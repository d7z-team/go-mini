package runtime

import (
	"context"
	"errors"
	"sync"

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

const completionDrainBudget = 256

type fiberQueue struct {
	items []*VMFiber
	head  int
}

func (q *fiberQueue) push(fiber *VMFiber) {
	if fiber == nil {
		return
	}
	q.items = append(q.items, fiber)
}

func (q *fiberQueue) pop() *VMFiber {
	if q.head >= len(q.items) {
		return nil
	}
	fiber := q.items[q.head]
	q.items[q.head] = nil
	q.head++
	if q.head == len(q.items) {
		q.reset()
		return fiber
	}
	if q.head > 64 && q.head*2 >= len(q.items) {
		copy(q.items, q.items[q.head:])
		tail := len(q.items) - q.head
		clear(q.items[tail:])
		q.items = q.items[:tail]
		q.head = 0
	}
	return fiber
}

func (q *fiberQueue) len() int {
	return len(q.items) - q.head
}

func (q *fiberQueue) reset() {
	clear(q.items)
	q.items = nil
	q.head = 0
}

func (q *fiberQueue) appendTo(dst []*VMFiber) []*VMFiber {
	if q.len() == 0 {
		return dst
	}
	return append(dst, q.items[q.head:]...)
}

type completionQueue struct {
	items []ffiCompletion
	head  int
}

func (q *completionQueue) push(completion ffiCompletion) {
	q.items = append(q.items, completion)
}

func (q *completionQueue) pop() (ffiCompletion, bool) {
	if q.head >= len(q.items) {
		return ffiCompletion{}, false
	}
	completion := q.items[q.head]
	q.items[q.head] = ffiCompletion{}
	q.head++
	if q.head == len(q.items) {
		q.reset()
		return completion, true
	}
	if q.head > 64 && q.head*2 >= len(q.items) {
		copy(q.items, q.items[q.head:])
		tail := len(q.items) - q.head
		clear(q.items[tail:])
		q.items = q.items[:tail]
		q.head = 0
	}
	return completion, true
}

func (q *completionQueue) len() int {
	return len(q.items) - q.head
}

func (q *completionQueue) reset() {
	clear(q.items)
	q.items = nil
	q.head = 0
}

type FiberScheduler struct {
	mu        sync.Mutex
	runID     uint64
	nextID    uint32
	nextToken uint64
	current   *VMFiber
	runq      fiberQueue
	pending   map[uint64]*suspendedFiber
	completed completionQueue
	accepted  map[uint64]struct{}
	wake      chan struct{}
	stopped   bool
}

func NewFiberScheduler() *FiberScheduler {
	return &FiberScheduler{
		pending:  make(map[uint64]*suspendedFiber),
		accepted: make(map[uint64]struct{}),
		wake:     make(chan struct{}, 1),
	}
}

func (s *FiberScheduler) Reset(root *StackContext, exec *Executor) (*VMFiber, error) {
	if s == nil || root == nil || exec == nil {
		return nil, errors.New("invalid fiber root")
	}
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runID++
	s.nextID = 1
	s.nextToken = 0
	s.current = nil
	s.runq.reset()
	s.pending = make(map[uint64]*suspendedFiber)
	s.completed.reset()
	s.accepted = make(map[uint64]struct{})
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	s.stopped = false
	rootFiber := &VMFiber{
		ID: s.nextID,
		Frames: []*FiberFrame{{
			Executor: exec,
			Session:  root,
			Kind:     FrameRoot,
		}},
	}
	s.runq.push(rootFiber)
	return rootFiber, nil
}

func (s *FiberScheduler) Current() *VMFiber {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *FiberScheduler) Go(session *StackContext, exec *Executor) (*VMFiber, error) {
	if s == nil || session == nil || exec == nil {
		return nil, errors.New("invalid go fiber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.runq.push(fiber)
	return fiber, nil
}

func (s *FiberScheduler) PushFrame(frame *FiberFrame) error {
	if s == nil {
		return errors.New("missing current fiber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return errors.New("missing current fiber")
	}
	return s.current.PushFrame(frame)
}

func (s *FiberScheduler) EnqueueFiber(fiber *VMFiber) {
	if s == nil || fiber == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.runq.push(fiber)
	s.signalLocked()
}

func (s *FiberScheduler) PrepareFFI(resume Task) (uint64, ffigo.WireCompletion, error) {
	if s == nil {
		return 0, nil, errors.New("missing current fiber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
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
	if s == nil {
		return errors.New("missing current fiber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, token)
	delete(s.accepted, token)
}

func (s *FiberScheduler) ParkCurrent() (*VMFiber, *FiberFrame, error) {
	if s == nil {
		return nil, nil, errors.New("missing current fiber")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
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
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	s.runq.push(s.current)
	s.current = nil
	s.signalLocked()
	return nil
}

func (s *FiberScheduler) FinishCurrent() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = nil
}

func (s *FiberScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancels := s.clearLocked()
	s.mu.Unlock()
	runFiberCancels(cancels)
}

func (s *FiberScheduler) AbortAll() []*VMFiber {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	fibers := make([]*VMFiber, 0, 1+s.runq.len()+len(s.pending))
	if s.current != nil {
		fibers = append(fibers, s.current)
	}
	fibers = s.runq.appendTo(fibers)
	for _, pending := range s.pending {
		if pending != nil && pending.Fiber != nil {
			fibers = append(fibers, pending.Fiber)
		}
	}
	cancels := s.clearLocked()
	s.mu.Unlock()
	runFiberCancels(cancels)
	return fibers
}

func (s *FiberScheduler) completeWire(token uint64, ret []byte, err error) bool {
	if s == nil {
		return false
	}
	var owned []byte
	if ret != nil {
		owned = append([]byte(nil), ret...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || token>>32 != s.runID {
		return false
	}
	if s.pending[token] == nil {
		return false
	}
	if _, exists := s.accepted[token]; exists {
		return false
	}
	s.completed.push(ffiCompletion{token: token, ret: owned, err: err})
	s.accepted[token] = struct{}{}
	s.signalLocked()
	return true
}

func (s *FiberScheduler) Next(ctx context.Context) (*VMFiber, error) {
	if s == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		fiber, done, wake := s.nextReady()
		if fiber != nil || done {
			return fiber, nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *FiberScheduler) nextReady() (*VMFiber, bool, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, true, nil
	}
	s.drainCompletionsLocked()
	if s.runq.len() > 0 {
		fiber := s.runq.pop()
		if s.completed.len() > 0 {
			s.signalLocked()
		}
		s.current = fiber
		return fiber, false, nil
	}
	if len(s.pending) == 0 {
		return nil, true, nil
	}
	if s.completed.len() > 0 {
		s.signalLocked()
	}
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	return nil, false, s.wake
}

func (s *FiberScheduler) drainCompletionsLocked() {
	for drained := 0; drained < completionDrainBudget; drained++ {
		completion, ok := s.completed.pop()
		if !ok {
			return
		}
		s.completeLocked(completion)
	}
}

func (s *FiberScheduler) completeLocked(completion ffiCompletion) {
	pending := s.pending[completion.token]
	if pending == nil {
		delete(s.accepted, completion.token)
		return
	}
	delete(s.pending, completion.token)
	delete(s.accepted, completion.token)
	if data, ok := pending.Resume.Data.(*ResumeFFIData); ok && data != nil {
		data.Ret = completion.ret
		data.Err = completion.err
	}
	pending.Frame.Session.TaskStack = append(pending.Frame.Session.TaskStack, pending.Resume)
	s.runq.push(pending.Fiber)
}

func (s *FiberScheduler) clearLocked() []func() {
	cancels := make([]func(), 0, len(s.pending))
	for _, pending := range s.pending {
		if pending != nil && pending.Cancel != nil {
			cancels = append(cancels, pending.Cancel)
		}
	}
	s.current = nil
	s.runq.reset()
	s.pending = make(map[uint64]*suspendedFiber)
	s.completed.reset()
	s.accepted = make(map[uint64]struct{})
	s.stopped = true
	s.signalLocked()
	return cancels
}

func (s *FiberScheduler) signalLocked() {
	if s.wake == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func runFiberCancels(cancels []func()) {
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
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
