package synclib_test

import (
	"context"
	"strings"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestWaitGroupSynchronizesExecutionContexts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prog := testutil.Program(t, `
package main
import "sync"
import "time"

func worker(wg *sync.WaitGroup, dst []int, idx int, value int) {
	dst[idx] = value
	wg.Done()
}

func main() {
	wg := sync.NewWaitGroup()
	values := []int{0, 0, 0}
	wg.Add(3)

	go worker(wg, values, 0, 11)
	go worker(wg, values, 1, 22)
	go worker(wg, values, 2, 33)

	wg.Wait()

	sum := values[0] + values[1] + values[2]
	if sum != 66 {
		panic("waitgroup did not observe child updates")
	}

	gate := sync.NewWaitGroup()
	ready := sync.NewWaitGroup()
	released := sync.NewWaitGroup()
	observed := 0

	gate.Add(1)
	ready.Add(2)
	released.Add(2)

	go func() {
		ready.Done()
		gate.Wait()
		observed = observed + 1
		released.Done()
	}()
	go func() {
		ready.Done()
		gate.Wait()
		observed = observed + 10
		released.Done()
	}()

	ready.Wait()
	gate.Done()
	released.Wait()
	if observed != 11 {
		panic("waitgroup did not release all waiters")
	}

	wg.Add(1)
	go func() {
		time.Sleep(1000000)
		values[0] = 100
		wg.Done()
	}()
	wg.Wait()
	if values[0] != 100 {
		panic("wait after reuse failed")
	}
}
`)
	if err := prog.Execute(ctx); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestWaitGroupNegativeCounterPanics(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
	prog, err := executor.NewRuntimeByGoCode(`
package main
import "sync"

func main() {
	wg := sync.NewWaitGroup()
	wg.Done()
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = prog.Execute(ctx)
	if err == nil || !strings.Contains(err.Error(), "negative WaitGroup counter") {
		t.Fatalf("expected negative counter panic, got %v", err)
	}
}
