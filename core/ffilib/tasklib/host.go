package tasklib

import (
	"context"
	"sync"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type Mutex struct {
	mu sync.Mutex
}

type (
	Task      struct{}
	TaskGroup struct{}
)

type Host struct{}

func (h *Host) NewTaskGroup() *TaskGroup {
	return &TaskGroup{}
}

func (h *Host) AddTask(group *TaskGroup, task *Task) {
}

func (h *Host) WaitTasks(ctx context.Context, group *TaskGroup) {
}

func (h *Host) GroupErr(group *TaskGroup) error {
	return nil
}

func (h *Host) CancelGroup(group *TaskGroup) {
}

func (h *Host) Status(task *Task) string {
	return ""
}

func (h *Host) Err(task *Task) error {
	return nil
}

func (h *Host) Cancel(task *Task) {
}

func (h *Host) Sleep(ctx context.Context, ms int64) {
	if ms <= 0 {
		return
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func (h *Host) NewMutex() *Mutex {
	return &Mutex{}
}

func (h *Host) Lock(ctx context.Context, mu *Mutex) {
	if mu == nil {
		return
	}
	locked := make(chan struct{})
	go func() {
		mu.mu.Lock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-ctx.Done():
	}
}

func (h *Host) Unlock(mu *Mutex) {
	if mu == nil {
		return
	}
	mu.mu.Unlock()
}

func RegisterTaskAll(executor interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
	RegisterStructSchema(string, *runtime.RuntimeStructSpec)
	RegisterConstant(string, string)
}, impl Module, registry *ffigo.HandleRegistry,
) {
	executor.RegisterStructSchema("task.Task", runtime.MustParseRuntimeStructSpec("task.Task", "struct {}"))
	executor.RegisterStructSchema("task.TaskGroup", runtime.MustParseRuntimeStructSpec("task.TaskGroup", "struct {}"))
	RegisterModule(executor, impl, registry)
}
