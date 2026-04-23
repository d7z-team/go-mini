package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type TaskStatus uint8

const (
	TaskPending TaskStatus = iota
	TaskRunning
	TaskSucceeded
	TaskFailed
	TaskCanceled
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskSucceeded:
		return "succeeded"
	case TaskFailed:
		return "failed"
	case TaskCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

type VMTask struct {
	ID uint32

	mu     sync.RWMutex
	status TaskStatus
	result *Var
	err    error

	cancel context.CancelFunc
	done   chan struct{}
}

type VMTaskGroup struct {
	mu       sync.Mutex
	taskIDs  map[uint32]struct{}
	firstErr error
}

func NewVMTaskGroup() *VMTaskGroup {
	return &VMTaskGroup{
		taskIDs: make(map[uint32]struct{}),
	}
}

func (g *VMTaskGroup) Add(taskID uint32) {
	if g == nil || taskID == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.taskIDs[taskID] = struct{}{}
}

func (g *VMTaskGroup) Snapshot() []uint32 {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ids := make([]uint32, 0, len(g.taskIDs))
	for id := range g.taskIDs {
		ids = append(ids, id)
	}
	return ids
}

func (g *VMTaskGroup) RememberErr(err error) {
	if g == nil || err == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.firstErr == nil {
		g.firstErr = err
	}
}

func (g *VMTaskGroup) Err() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.firstErr
}

func (t *VMTask) setResult(status TaskStatus, result *Var, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status = status
	if result != nil {
		t.result = result.Copy()
	} else {
		t.result = nil
	}
	t.err = err
}

func (t *VMTask) snapshot() (TaskStatus, *Var, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.result != nil {
		return t.status, t.result.Copy(), t.err
	}
	return t.status, nil, t.err
}

type TaskScheduler struct {
	mu           sync.Mutex
	nextID       uint32
	tasks        map[uint32]*VMTask
	shuttingDown bool
}

func NewTaskScheduler() *TaskScheduler {
	return &TaskScheduler{
		tasks: make(map[uint32]*VMTask),
	}
}

func (s *TaskScheduler) BeginRoot() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shuttingDown = false
	for id, task := range s.tasks {
		if task == nil {
			delete(s.tasks, id)
			continue
		}
		select {
		case <-task.done:
			delete(s.tasks, id)
		default:
		}
	}
}

func (s *TaskScheduler) Spawn(parent context.Context, run func(context.Context) (*Var, error)) (*VMTask, error) {
	if s == nil || run == nil {
		return nil, errors.New("invalid task spawn")
	}
	if parent == nil {
		parent = context.Background()
	}

	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return nil, errors.New("cannot spawn task during shutdown")
	}
	s.nextID++
	id := s.nextID
	ctx, cancel := context.WithCancel(parent)
	task := &VMTask{
		ID:     id,
		status: TaskPending,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.tasks[id] = task
	s.mu.Unlock()

	go func() {
		task.setResult(TaskRunning, nil, nil)
		result, err := run(ctx)
		status := TaskSucceeded
		if ctx.Err() != nil {
			status = TaskCanceled
			err = ctx.Err()
			result = nil
		} else if err != nil {
			status = TaskFailed
		}
		task.setResult(status, result, err)
		cancel()
		close(task.done)
	}()

	return task, nil
}

func (s *TaskScheduler) Await(ctx context.Context, id uint32) (*Var, error) {
	task, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-task.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	status, result, taskErr := task.snapshot()
	switch status {
	case TaskSucceeded:
		return result, nil
	case TaskCanceled:
		if taskErr != nil {
			return nil, taskErr
		}
		return nil, context.Canceled
	case TaskFailed:
		if taskErr != nil {
			return nil, taskErr
		}
		return nil, errors.New("task failed")
	default:
		return nil, fmt.Errorf("task status %s", status.String())
	}
}

func (s *TaskScheduler) Cancel(id uint32) error {
	task, err := s.lookup(id)
	if err != nil {
		return err
	}
	if task.cancel != nil {
		task.cancel()
	}
	return nil
}

func (s *TaskScheduler) Status(id uint32) (TaskStatus, error) {
	task, err := s.lookup(id)
	if err != nil {
		return TaskCanceled, err
	}
	status, _, _ := task.snapshot()
	return status, nil
}

func (s *TaskScheduler) Error(id uint32) (TaskStatus, error, error) {
	task, err := s.lookup(id)
	if err != nil {
		return TaskCanceled, nil, err
	}
	status, _, taskErr := task.snapshot()
	return status, taskErr, nil
}

func (s *TaskScheduler) lookup(id uint32) (*VMTask, error) {
	if s == nil || id == 0 {
		return nil, errors.New("invalid task handle")
	}
	s.mu.Lock()
	task, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok || task == nil {
		return nil, fmt.Errorf("task handle %d not found", id)
	}
	return task, nil
}

func (s *TaskScheduler) ShutdownFromRoot() {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.shuttingDown = true
	tasks := make([]*VMTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}
	s.mu.Unlock()

	for _, task := range tasks {
		if task != nil && task.cancel != nil {
			task.cancel()
		}
	}
}
