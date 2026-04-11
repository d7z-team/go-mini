package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestGoTaskRootShutdownCancelsPendingTask(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

var done = false

func worker() {
	task.Sleep(200)
	done = true
}

func main() {
	go worker()
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}

	snapshot := prog.SharedState()
	done, ok := snapshot.LoadGlobal("done")
	if !ok {
		t.Fatal("missing global done")
	}
	if done == nil || done.Bool {
		t.Fatalf("expected pending task to be canceled before setting done, got %#v", done)
	}
}

func TestTaskGroupWaitsForWorker(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

var done = false

func worker() {
	task.Sleep(10)
	done = true
}

func main() {
	g := task.NewTaskGroup()
	t := spawn(worker)
	task.AddTask(g, t)
	task.WaitTasks(g)
	if task.GroupErr(g) != nil {
		panic("unexpected task group error")
	}
	if !done {
		panic("worker did not finish")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTaskGroupCapturesFirstError(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

func fail() Int64 {
	panic("boom")
}

func main() {
	g := task.NewTaskGroup()
	t := spawn(fail)
	task.AddTask(g, t)
	task.WaitTasks(g)
	if task.GroupErr(g) == nil {
		panic("expected task group error")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSpawnAwaitReturnsTaskResult(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

func work() Int64 {
	return 42
}

func main() {
	t := spawn(work)
	if task.Status(t) != "pending" && task.Status(t) != "running" && task.Status(t) != "succeeded" {
		panic("unexpected task status")
	}
	if await(t) != 42 {
		panic("await mismatch")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTaskCancelCancelsAwaitedTask(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

func work() Int64 {
	task.Sleep(200)
	return 42
}

func main() {
	t := spawn(work)
	task.Cancel(t)
	if task.Status(t) != "pending" && task.Status(t) != "running" && task.Status(t) != "canceled" {
		panic("unexpected task status before await")
	}
	if await(t) != 0 {
		panic("expected await to panic on canceled task")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err == nil {
		t.Fatal("expected canceled await to return error")
	}
}

func TestTaskErrObservesFailureAndCancelWithoutAwait(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

func fail() Int64 {
	panic("boom")
}

func sleep() Int64 {
	task.Sleep(200)
	return 42
}

func main() {
	failed := spawn(fail)
	canceled := spawn(sleep)
	task.Cancel(canceled)

	task.Sleep(10)

	if task.Err(failed) == nil {
		panic("expected failed task error")
	}
	if task.Err(canceled) == nil {
		panic("expected canceled task error")
	}

	ok := spawn(func() Int64 { return 7 })
	if task.Err(ok) != nil {
		panic("successful task should not have error")
	}
	if await(ok) != 7 {
		panic("await mismatch")
	}
	if task.Err(ok) != nil {
		panic("successful completed task should not expose error")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
