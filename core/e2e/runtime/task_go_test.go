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

func TestSpawnSnapshotCapturesScalarAndContainerValues(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

func main() {
	x := 1
	arr := []Int64{7}

	t := spawn(func() Int64 {
		arr[0] = 99
		return x + arr[0]
	})

	x = 100
	arr[0] = 5

	if await(t) != 100 {
		panic("snapshot mismatch")
	}
	if x != 100 {
		panic("parent scalar should keep its own value")
	}
	if arr[0] != 5 {
		panic("parent container should not be mutated by child")
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

func TestSpawnSnapshotChildWriteDoesNotEscapeCapturedState(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

func main() {
	x := 3
	t := spawn(func() Int64 {
		x = 11
		return x
	})
	if await(t) != 11 {
		panic("child should see its own updated snapshot")
	}
	if x != 3 {
		panic("child write should not leak to parent")
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

func TestSpawnSnapshotAllowsCapturedTaskHandle(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

func work() Int64 {
	return 1
}

func main() {
	base := spawn(work)
	t := spawn(func() Int64 {
		if task.Status(base) == "" {
			panic("missing status")
		}
		return await(base)
	})
	if await(t) != 1 {
		panic("captured task handle should keep identity")
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

func TestSpawnSnapshotAllowsCapturedHostHandle(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

import "task"

func main() {
	mu := task.NewMutex()
	t := spawn(func() Int64 {
		task.Lock(mu)
		task.Unlock(mu)
		return 7
	})
	if await(t) != 7 {
		panic("captured host handle should keep identity")
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

func TestSpawnSnapshotRejectsCapturedVMPointer(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
package main

func main() {
	p := new(Int64)
	_ = spawn(func() Int64 {
		*p = 5
		return *p
	})
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err == nil {
		t.Fatal("expected captured VM pointer snapshot to fail")
	}
}
