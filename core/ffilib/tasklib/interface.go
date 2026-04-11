//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tasklib -out task_ffigen.go interface.go
package tasklib

import "context"

// ffigen:module task
type Module interface {
	NewTaskGroup() *TaskGroup
	AddTask(group *TaskGroup, task *Task)
	WaitTasks(ctx context.Context, group *TaskGroup)
	GroupErr(group *TaskGroup) error
	CancelGroup(group *TaskGroup)
	Status(task *Task) string
	Err(task *Task) error
	Cancel(task *Task)
	Sleep(ctx context.Context, ms int64)
	NewMutex() *Mutex
	Lock(ctx context.Context, mu *Mutex)
	Unlock(mu *Mutex)
}
