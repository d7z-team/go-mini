package runtime

import (
	"context"
	"errors"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ExecutionContextFrameKind uint8

const (
	FrameRoot ExecutionContextFrameKind = iota
	FrameGo
	FrameModuleInit
)

type ExecutionContextFrame struct {
	Executor *Executor
	Session  *StackContext
	Kind     ExecutionContextFrameKind
	OnDone   func(*ExecutionContextFrame) error
	OnError  func(*ExecutionContextFrame, error) error
	Cleanup  bool
}

type VMExecutionContext struct {
	ID     uint32
	Frames []*ExecutionContextFrame
}

func (f *VMExecutionContext) CurrentFrame() *ExecutionContextFrame {
	if f == nil || len(f.Frames) == 0 {
		return nil
	}
	return f.Frames[len(f.Frames)-1]
}

func (f *VMExecutionContext) PushFrame(frame *ExecutionContextFrame) error {
	if f == nil || frame == nil || frame.Executor == nil || frame.Session == nil {
		return errors.New("invalid VM execution context frame")
	}
	f.Frames = append(f.Frames, frame)
	return nil
}

func (f *VMExecutionContext) PopFrame() *ExecutionContextFrame {
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

type suspendedExecutionContext struct {
	ExecutionContext *VMExecutionContext
	Frame            *ExecutionContextFrame
	Resume           Task
	Cancel           func()
}

const completionDrainBudget = 256

type executionContextQueue struct {
	items []*VMExecutionContext
	head  int
}

func (q *executionContextQueue) push(execCtx *VMExecutionContext) {
	if execCtx == nil {
		return
	}
	q.items = append(q.items, execCtx)
}

func (q *executionContextQueue) pop() *VMExecutionContext {
	if q.head >= len(q.items) {
		return nil
	}
	execCtx := q.items[q.head]
	q.items[q.head] = nil
	q.head++
	if q.head == len(q.items) {
		q.reset()
		return execCtx
	}
	if q.head > 64 && q.head*2 >= len(q.items) {
		copy(q.items, q.items[q.head:])
		tail := len(q.items) - q.head
		clear(q.items[tail:])
		q.items = q.items[:tail]
		q.head = 0
	}
	return execCtx
}

func (q *executionContextQueue) len() int {
	return len(q.items) - q.head
}

func (q *executionContextQueue) reset() {
	clear(q.items)
	q.items = nil
	q.head = 0
}

func (q *executionContextQueue) appendTo(dst []*VMExecutionContext) []*VMExecutionContext {
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

type ExecutionContextScheduler struct {
	mu        sync.Mutex
	runID     uint64
	nextID    uint32
	nextToken uint64
	current   *VMExecutionContext
	runq      executionContextQueue
	pending   map[uint64]*suspendedExecutionContext
	completed completionQueue
	accepted  map[uint64]struct{}
	wake      chan struct{}
	stopped   bool
}

func NewExecutionContextScheduler() *ExecutionContextScheduler {
	return &ExecutionContextScheduler{
		pending:  make(map[uint64]*suspendedExecutionContext),
		accepted: make(map[uint64]struct{}),
		wake:     make(chan struct{}, 1),
	}
}

func (s *ExecutionContextScheduler) Reset(root *StackContext, exec *Executor) (*VMExecutionContext, error) {
	if s == nil || root == nil || exec == nil {
		return nil, errors.New("invalid root VM execution context")
	}
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runID++
	s.nextID = 1
	s.nextToken = 0
	s.current = nil
	s.runq.reset()
	s.pending = make(map[uint64]*suspendedExecutionContext)
	s.completed.reset()
	s.accepted = make(map[uint64]struct{})
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	s.stopped = false
	rootExecCtx := &VMExecutionContext{
		ID: s.nextID,
		Frames: []*ExecutionContextFrame{{
			Executor: exec,
			Session:  root,
			Kind:     FrameRoot,
		}},
	}
	s.runq.push(rootExecCtx)
	return rootExecCtx, nil
}

func (s *ExecutionContextScheduler) Current() *VMExecutionContext {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *ExecutionContextScheduler) Go(session *StackContext, exec *Executor) (*VMExecutionContext, error) {
	if s == nil || session == nil || exec == nil {
		return nil, errors.New("invalid go execution context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, errors.New("cannot start go execution context after scheduler stopped")
	}
	s.nextID++
	execCtx := &VMExecutionContext{
		ID: s.nextID,
		Frames: []*ExecutionContextFrame{{
			Executor: exec,
			Session:  session,
			Kind:     FrameGo,
			Cleanup:  true,
		}},
	}
	s.runq.push(execCtx)
	return execCtx, nil
}

func (s *ExecutionContextScheduler) PushFrame(frame *ExecutionContextFrame) error {
	if s == nil {
		return errors.New("missing current VM execution context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return errors.New("missing current VM execution context")
	}
	return s.current.PushFrame(frame)
}

func (s *ExecutionContextScheduler) EnqueueExecutionContext(execCtx *VMExecutionContext) {
	if s == nil || execCtx == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.runq.push(execCtx)
	s.signalLocked()
}

func (s *ExecutionContextScheduler) PrepareFFI(resume Task) (uint64, ffigo.WireCompletion, error) {
	if s == nil {
		return 0, nil, errors.New("missing current VM execution context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return 0, nil, errors.New("missing current VM execution context")
	}
	frame := s.current.CurrentFrame()
	if frame == nil {
		return 0, nil, errors.New("missing current VM execution context frame")
	}
	s.nextToken++
	token := (s.runID << 32) | s.nextToken
	s.pending[token] = &suspendedExecutionContext{
		ExecutionContext: s.current,
		Frame:            frame,
		Resume:           resume,
	}
	return token, ffiCompletionSink{scheduler: s, token: token}, nil
}

func (s *ExecutionContextScheduler) CommitFFI(token uint64, cancel func()) error {
	if s == nil {
		return errors.New("missing current VM execution context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return errors.New("missing current VM execution context")
	}
	pending := s.pending[token]
	if pending == nil {
		return errors.New("missing pending FFI execution context")
	}
	pending.Cancel = cancel
	s.current = nil
	return nil
}

func (s *ExecutionContextScheduler) AbortFFI(token uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, token)
	delete(s.accepted, token)
}

func (s *ExecutionContextScheduler) ParkCurrent() (*VMExecutionContext, *ExecutionContextFrame, error) {
	if s == nil {
		return nil, nil, errors.New("missing current VM execution context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil, nil, errors.New("missing current VM execution context")
	}
	frame := s.current.CurrentFrame()
	if frame == nil {
		return nil, nil, errors.New("missing current VM execution context frame")
	}
	execCtx := s.current
	s.current = nil
	return execCtx, frame, nil
}

func (s *ExecutionContextScheduler) YieldCurrent() error {
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

func (s *ExecutionContextScheduler) FinishCurrent() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = nil
}

func (s *ExecutionContextScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancels := s.clearLocked()
	s.mu.Unlock()
	runExecutionContextCancels(cancels)
}

func (s *ExecutionContextScheduler) AbortAll() []*VMExecutionContext {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	execCtxs := make([]*VMExecutionContext, 0, 1+s.runq.len()+len(s.pending))
	if s.current != nil {
		execCtxs = append(execCtxs, s.current)
	}
	execCtxs = s.runq.appendTo(execCtxs)
	for _, pending := range s.pending {
		if pending != nil && pending.ExecutionContext != nil {
			execCtxs = append(execCtxs, pending.ExecutionContext)
		}
	}
	cancels := s.clearLocked()
	s.mu.Unlock()
	runExecutionContextCancels(cancels)
	return execCtxs
}

func (s *ExecutionContextScheduler) completeWire(token uint64, ret []byte, err error) bool {
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

func (s *ExecutionContextScheduler) Next(ctx context.Context) (*VMExecutionContext, error) {
	if s == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		execCtx, done, wake := s.nextReady()
		if execCtx != nil || done {
			return execCtx, nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *ExecutionContextScheduler) nextReady() (*VMExecutionContext, bool, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, true, nil
	}
	s.drainCompletionsLocked()
	if s.runq.len() > 0 {
		execCtx := s.runq.pop()
		if s.completed.len() > 0 {
			s.signalLocked()
		}
		s.current = execCtx
		return execCtx, false, nil
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

func (s *ExecutionContextScheduler) drainCompletionsLocked() {
	for drained := 0; drained < completionDrainBudget; drained++ {
		completion, ok := s.completed.pop()
		if !ok {
			return
		}
		s.completeLocked(completion)
	}
}

func (s *ExecutionContextScheduler) completeLocked(completion ffiCompletion) {
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
	s.runq.push(pending.ExecutionContext)
}

func (s *ExecutionContextScheduler) clearLocked() []func() {
	cancels := make([]func(), 0, len(s.pending))
	for _, pending := range s.pending {
		if pending != nil && pending.Cancel != nil {
			cancels = append(cancels, pending.Cancel)
		}
	}
	s.current = nil
	s.runq.reset()
	s.pending = make(map[uint64]*suspendedExecutionContext)
	s.completed.reset()
	s.accepted = make(map[uint64]struct{})
	s.stopped = true
	s.signalLocked()
	return cancels
}

func (s *ExecutionContextScheduler) signalLocked() {
	if s.wake == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func runExecutionContextCancels(cancels []func()) {
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
}

type ffiCompletionSink struct {
	scheduler *ExecutionContextScheduler
	token     uint64
}

func (s ffiCompletionSink) CompleteWire(ret []byte, err error) bool {
	if s.scheduler == nil {
		return false
	}
	return s.scheduler.completeWire(s.token, ret, err)
}
